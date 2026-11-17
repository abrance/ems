package spotreport

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"application/api/spotreport/v1"
	"application/internal/logic/homemarket"
	"s_db/s_db_model"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
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
			run_date VARCHAR(10) NOT NULL, city_code VARCHAR(16) NOT NULL,
			city_name VARCHAR(32) NOT NULL,
			period_type INTEGER NOT NULL DEFAULT 1, price REAL NOT NULL DEFAULT 0,
			source VARCHAR(64) NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP, UNIQUE (run_date, city_code, period_type))`, `
		CREATE TABLE gd_home_market_fetch_run (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_date VARCHAR(10) NOT NULL, dataset VARCHAR(16) NOT NULL,
			status VARCHAR(16) NOT NULL DEFAULT '', error_kind VARCHAR(16) NOT NULL DEFAULT '',
			rows_written INTEGER NOT NULL DEFAULT 0, rows_expected INTEGER NOT NULL DEFAULT 0,
			attempt_count INTEGER NOT NULL DEFAULT 0,
			consecutive_failures INTEGER NOT NULL DEFAULT 0,
			consecutive_empties INTEGER NOT NULL DEFAULT 0,
			last_error TEXT,
			first_seen_at DATETIME DEFAULT CURRENT_TIMESTAMP, last_ok_at DATETIME,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (target_date, dataset))`} {
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
			code, _ := homemarket.CityCodeOf(c)
			if _, err := g.DB().Exec(ctx, `INSERT INTO gd_node_price
				(run_date,city_code,city_name,period_type,price,source) VALUES (?,?,?,?,?,?)`,
				"2026-11-13", code, c, tp, 465.0, "官网·节点价"); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// shiftHomeMarketDates 把夹具的两路数据平移到「homeDays/nodeDays 天前」，
// 供依赖 now 的滞后判定类用例使用。夹具的派生用例（断言具体日期字符串的）
// 不要调它。
func shiftHomeMarketDates(t *testing.T, homeDays, nodeDays int) {
	t.Helper()
	ctx := context.Background()
	if _, err := g.DB().Exec(ctx, `UPDATE gd_home_24h SET price_date = ?`,
		time.Now().AddDate(0, 0, -homeDays).Format("2006-01-02")); err != nil {
		t.Fatalf("平移 home24h 日期失败: %v", err)
	}
	if _, err := g.DB().Exec(ctx, `UPDATE gd_node_price SET run_date = ?`,
		time.Now().AddDate(0, 0, -nodeDays).Format("2006-01-02")); err != nil {
		t.Fatalf("平移 nodePrice 日期失败: %v", err)
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
	if got["pointCount"] != float64(24) {
		t.Fatalf("pointCount = %v，期望 24", got["pointCount"])
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

// date 格式错必须是参数错误，不能变成 empty:true —— 否则前端分不清「填错」和「源站没发布」。
func TestGetHome24hRejectsBadDate(t *testing.T) {
	setupHomeMarketDB(t)
	c := &ControllerV1{}

	_, err := c.GetHome24h(context.Background(), &v1.GetHome24hReq{Date: "2026/11/13"})
	if err == nil {
		t.Fatal("date=2026/11/13 应报参数错误")
	}
	if code := gerror.Code(err); code != gcode.CodeInvalidParameter {
		t.Fatalf("code = %v，期望 CodeInvalidParameter（%v）", code, err)
	}

	// 地市端点同一口径
	_, err = c.GetNodePrice(context.Background(), &v1.GetNodePriceReq{Date: "2026-13-01"})
	if code := gerror.Code(err); code != gcode.CodeInvalidParameter {
		t.Fatalf("node-price code = %v，期望 CodeInvalidParameter（%v）", code, err)
	}
}

// 缺点日：全天统计不下发，但 pointCount 要说清楚点数。
func TestGetHome24hPartialDayOmitsAllDayStats(t *testing.T) {
	setupHomeMarketDB(t)
	if _, err := g.DB().Exec(context.Background(),
		`DELETE FROM gd_home_24h WHERE price_date = '2026-11-07' AND hour >= 17`); err != nil {
		t.Fatalf("删点失败: %v", err)
	}

	c := &ControllerV1{}
	res, err := c.GetHome24h(context.Background(), &v1.GetHome24hReq{})
	if err != nil {
		t.Fatalf("GetHome24h 失败: %v", err)
	}
	got := jsonShape(t, res)
	if got["pointCount"] != float64(17) {
		t.Fatalf("pointCount = %v，期望 17", got["pointCount"])
	}
	for _, k := range []string{"meanAhead", "meanRealtime", "peakPowerMw", "lowPowerMw"} {
		if v, ok := got[k]; ok {
			t.Fatalf("%s 不该下发（17 点不是全天），实际 %v", k, v)
		}
	}
}

// 状态端点：前端 status 卡片读的字段一个都不能少，且**不能带数值**。
//
// 为什么专测「不许带数值」：这个端点的用途是「源站挂了别让页面以为价格是 0」，
// 一旦有人顺手把 points/price 塞进来，页面就有机会在失败时画出 0 元的曲线。
func TestGetHomeMarketStatusShape(t *testing.T) {
	setupHomeMarketDB(t)
	// 状态里的 lagDays 是相对 now 现算的，而夹具用的是固定日期，跑到某天就必然
	// 超出 expectedLag（home24h 8 天、nodePrice 2 天），断言「state=ok」会自然过期。
	// 这里把两路数据落到最近，让本用例只验形状与「滞后在预期内」的口径。
	shiftHomeMarketDates(t, 3, 1)

	c := &ControllerV1{}
	res, err := c.GetHomeMarketStatus(context.Background(), &v1.GetHomeMarketStatusReq{})
	if err != nil {
		t.Fatalf("GetHomeMarketStatus 失败: %v", err)
	}
	got := jsonShape(t, res)

	for _, ds := range []string{"home24h", "nodePrice"} {
		raw, ok := got[ds].(map[string]any)
		if !ok {
			t.Fatalf("响应缺 %q 段：%v", ds, got)
		}
		for _, k := range []string{"dataset", "state", "asOf", "fetchedAt", "lagDays", "rows", "rowsExpected", "lastError", "errorKind"} {
			if _, ok := raw[k]; !ok {
				t.Fatalf("%s 缺字段 %q：%v", ds, k, raw)
			}
		}
		// 状态端点只讲「能不能用」，不许出现任何数值字段
		for _, banned := range []string{"points", "price", "aheadPrice", "realtimePrice"} {
			if _, ok := raw[banned]; ok {
				t.Fatalf("%s 不该带数值字段 %q（失败时页面会画出 0）", ds, banned)
			}
		}
	}

	// 这个库里的数据是「有数、没跑过采集」：状态是 ok（滞后在预期内），
	// 但 fetchedAt 为空、errorKind/error 也为空 —— 与「跑过且成功」区分开。
	home := got["home24h"].(map[string]any)
	if home["state"] != "ok" {
		t.Fatalf("home24h state = %v，期望 ok（24 点齐、滞后在预期内）", home["state"])
	}
	if home["dataset"] != "home24h" || home["rows"] != float64(24) || home["rowsExpected"] != float64(24) {
		t.Fatalf("home24h dataset/rows 错: %v", home)
	}
	if home["kind"] != nil {
		t.Fatalf("多余字段: %v", home)
	}
	node := got["nodePrice"].(map[string]any)
	if node["rows"] != float64(42) || node["rowsExpected"] != float64(42) {
		t.Fatalf("nodePrice rows = %v/%v，期望 42/42", node["rows"], node["rowsExpected"])
	}
}
