package spotreport

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"application/api/spotreport/v1"
	"s_db/s_db_model"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// 端到端（控制器 → 逻辑 → 真 sqlite）：验的是**前端读到的 JSON 形状**。
//
// 逻辑层单测守不住这件事：前端读 date / empty / latestDate / points，
// 字段名写错或 nil 切片序列化成 null，逻辑层全绿而页面直接崩。
// 这里不经过 HTTP：cmd 包的路由测试只验「路由注册了」，响应中间件在
// 单元测试环境里（无完整 session/设备管理器）对所有接口都写空 body，
// 拿它验 JSON 形状只会验到测试环境的坑，验不到接口。
func setupHomeMarketDB(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db.sqlite3")
	gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	})
	t.Cleanup(func() { gdb.SetConfig(gdb.Config{}) })

	ctx := context.Background()
	for _, ddl := range []string{`
		CREATE TABLE gd_home_24h (
			price_date VARCHAR(10) NOT NULL, hour INTEGER NOT NULL,
			power_mw REAL NOT NULL DEFAULT 0, ahead_price REAL NOT NULL DEFAULT 0,
			realtime_price REAL NOT NULL DEFAULT 0, source VARCHAR(64) NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP, UNIQUE (price_date, hour))`, `
		CREATE TABLE gd_node_price (
			run_date VARCHAR(10) NOT NULL, city VARCHAR(32) NOT NULL,
			period_type INTEGER NOT NULL DEFAULT 1, price REAL NOT NULL DEFAULT 0,
			source VARCHAR(64) NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP, UNIQUE (run_date, city, period_type))`} {
		if _, err := g.DB().Exec(ctx, ddl); err != nil {
			t.Fatalf("建表失败: %v", err)
		}
	}
	for h := 0; h < 24; h++ {
		if _, err := g.DB().Exec(ctx, `INSERT INTO gd_home_24h
			(price_date,hour,power_mw,ahead_price,realtime_price,source) VALUES (?,?,?,?,?,?)`,
			"2026-11-07", h, 60000.0, 400.0, 500.0, "官网·24点"); err != nil {
			t.Fatal(err)
		}
	}
	cities := []string{"东莞市", "中山市", "云浮市", "佛山市", "广州市", "惠州市", "揭阳市", "梅州市",
		"汕头市", "汕尾市", "江门市", "河源市", "深圳市", "清远市", "湛江市", "潮州市",
		"珠海市", "肇庆市", "茂名市", "阳江市", "韶关市"}
	for _, c := range cities {
		for _, tp := range []int{s_db_model.NodePricePeriodPeak, s_db_model.NodePricePeriodValley} {
			if _, err := g.DB().Exec(ctx, `INSERT INTO gd_node_price
				(run_date,city,period_type,price,source) VALUES (?,?,?,?,?)`,
				"2026-11-13", c, tp, 465.0, "官网·节点价"); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// jsonShape 把响应结构体转成前端真正读到的 JSON 再解回来
func jsonShape(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	return out
}

func TestGetHome24hEndpointShape(t *testing.T) {
	setupHomeMarketDB(t)
	c := &ControllerV1{}
	res, err := c.GetHome24h(context.Background(), &v1.GetHome24hReq{})
	if err != nil {
		t.Fatalf("GetHome24h 失败: %v", err)
	}
	got := jsonShape(t, res)

	// 前端 api/homemarket.ts 读的字段一个都不能少
	for _, k := range []string{"date", "empty", "latestDate", "lagDays", "points", "source", "meanAhead", "meanRealtime"} {
		if _, ok := got[k]; !ok {
			t.Fatalf("响应缺字段 %q：%v", k, got)
		}
	}
	if got["empty"] != false {
		t.Fatalf("empty = %v，期望 false", got["empty"])
	}
	if got["date"] != "2026-11-07" || got["latestDate"] != "2026-11-07" {
		t.Fatalf("日期字段错: %v / %v", got["date"], got["latestDate"])
	}
	pts, ok := got["points"].([]any)
	if !ok {
		t.Fatalf("points 不是数组（Go nil 切片会序列化成 null，页面 .map 直接崩）: %T", got["points"])
	}
	if len(pts) != 24 {
		t.Fatalf("点数 = %d，期望 24", len(pts))
	}
	first, _ := pts[0].(map[string]any)
	for _, k := range []string{"hour", "powerMw", "aheadPrice", "realtimePrice"} {
		if _, ok := first[k]; !ok {
			t.Fatalf("点缺字段 %q: %v", k, first)
		}
	}
}

func TestGetHome24hEmptyDayShape(t *testing.T) {
	setupHomeMarketDB(t)
	c := &ControllerV1{}
	// 显式指定一个源站还没发布的日期：页面靠 empty + latestDate 说「该日无数据」
	res, err := c.GetHome24h(context.Background(), &v1.GetHome24hReq{Date: "2026-11-08"})
	if err != nil {
		t.Fatalf("空日期不该报错: %v", err)
	}
	got := jsonShape(t, res)
	if got["empty"] != true {
		t.Fatalf("empty = %v，期望 true", got["empty"])
	}
	if got["latestDate"] != "2026-11-07" {
		t.Fatalf("latestDate = %v，期望 2026-11-07（页面靠它提示最新到哪天）", got["latestDate"])
	}
	if pts, ok := got["points"].([]any); !ok || len(pts) != 0 {
		t.Fatalf("空日期 points 必须是 []（不是 null），实际 %T %v", got["points"], got["points"])
	}
}

func TestGetNodePriceEndpointShape(t *testing.T) {
	setupHomeMarketDB(t)
	c := &ControllerV1{}
	res, err := c.GetNodePrice(context.Background(), &v1.GetNodePriceReq{})
	if err != nil {
		t.Fatalf("GetNodePrice 失败: %v", err)
	}
	got := jsonShape(t, res)
	for _, k := range []string{"date", "empty", "latestDate", "lagDays", "points", "source", "peakCities", "valleyCities"} {
		if _, ok := got[k]; !ok {
			t.Fatalf("响应缺字段 %q：%v", k, got)
		}
	}
	if got["date"] != "2026-11-13" {
		t.Fatalf("date = %v，期望 2026-11-13", got["date"])
	}
	pts, ok := got["points"].([]any)
	if !ok {
		t.Fatalf("points 不是数组: %T", got["points"])
	}
	if len(pts) != 42 {
		t.Fatalf("条数 = %d，期望 42（21 市 × 2 时段）", len(pts))
	}
	if got["peakCities"] != float64(21) || got["valleyCities"] != float64(21) {
		t.Fatalf("高峰/低谷城市数 = %v/%v", got["peakCities"], got["valleyCities"])
	}
	first, _ := pts[0].(map[string]any)
	for _, k := range []string{"city", "periodType", "price"} {
		if _, ok := first[k]; !ok {
			t.Fatalf("点缺字段 %q: %v", k, first)
		}
	}
}
