package forecast

import (
	"context"
	"os"
	"testing"
	"time"

	v1 "application/api/forecast/v1"

	"github.com/gogf/gf/v2/database/gdb"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
)

// 留痕对账在真库上的行为：真值走官方日频、回填行被剔除、96 点不给假真值。
//
// 用法：
//
//	GDPM_DB=/path/to/db.sqlite3 go test ./internal/controller/forecast/ -run TestGetRecordedForecastAgainstRealDB -v
func TestGetRecordedForecastAgainstRealDB(t *testing.T) {
	path := os.Getenv("GDPM_DB")
	if path == "" {
		t.Skip("需要 GDPM_DB（ems 的 sqlite 库副本），跳过")
	}
	gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	})
	t.Cleanup(func() { gdb.SetConfig(gdb.Config{}) })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := &ControllerV1{}

	// 日频：真值必须与日频回测腿同一口径（官方最新实际值日），不能停在第三方 CSV 的 10-02
	day, err := c.GetRecordedForecast(ctx, &v1.GetRecordedForecastReq{
		From: "2026-09-20", To: "2026-11-11", Market: "ahead", Grain: "day",
	})
	if err != nil {
		t.Fatalf("留痕（日频）失败: %v", err)
	}
	if day.DataAsOf == "" {
		t.Fatal("DataAsOf 为空：页面无法算滞后档位")
	}
	if day.DataAsOf < "2026-11-01" {
		t.Fatalf("DataAsOf = %s，真值不是官方日频最新日", day.DataAsOf)
	}
	if day.TruthSource == "" {
		t.Fatal("TruthSource 为空：页面脚注要原样展示来源")
	}
	t.Logf("真值最新到 %s（滞后 %d 天），留痕 %d 条，剔除回填 %d 条，缺实际值 %d 条",
		day.DataAsOf, day.LagDays, len(day.Points), day.Backfilled, day.ActualMissing)
	for _, p := range day.Points {
		if p.GeneratedOn > p.TargetTime[:len("2006-01-02")] {
			t.Fatalf("回填行没被剔除: 生成 %s 目标 %s", p.GeneratedOn, p.TargetTime)
		}
		if p.Actual == nil {
			t.Fatalf("留痕点 %s 没有配对真值", p.TargetTime)
		}
	}

	// 96 点：官方没有分时真值 → 必须明说不可配对，而不是拿日频硬凑
	intra, err := c.GetRecordedForecast(ctx, &v1.GetRecordedForecastReq{
		From: "2026-09-01 00:00", To: "2026-09-06 23:59", Market: "ahead", Grain: "intraday",
	})
	if err != nil {
		t.Fatalf("留痕（96 点）失败: %v", err)
	}
	if intra.ActualUnavailable == "" {
		t.Fatal("96 点留痕应说明没有官方分时真值可配对")
	}
	for _, p := range intra.Points {
		if p.Actual != nil {
			t.Fatalf("96 点留痕不允许配日频真值: %s", p.TargetTime)
		}
	}
}
