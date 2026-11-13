package forecast

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gogf/gf/v2/database/gdb"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
)

// 真库上的官方日频真值读取：留痕对账腿与日频回测腿共用这一条路径。
//
// 用法：
//
//	GDPM_DB=/path/to/db.sqlite3 go test ./internal/logic/forecast/ -run TestLoadDailyActualsAgainstRealDB -v
//
// 为什么必须在真库上跑：指标名、scope、来源优先级都只在真实行里才有；
// 用构造行测会「测通一个不存在的口径」。
func TestLoadDailyActualsAgainstRealDB(t *testing.T) {
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

	series, err := LoadDailyActuals(ctx, "ahead", "2026-09-01", "2026-11-13")
	if err != nil {
		t.Fatalf("读取官方日均实际值失败: %v", err)
	}
	if series.DataAsOf == "" {
		t.Fatal("DataAsOf 为空：真值读不到时页面无法算陈旧档位")
	}
	// 右端放到今天，所以最新日必须远晚于查询窗口起点（否则说明只返回了窗口尾部）
	if series.DataAsOf < "2026-11-01" {
		t.Fatalf("DataAsOf = %s，看起来只取到窗口内数据而不是真值最新日", series.DataAsOf)
	}
	if v, ok := series.ByDate[series.DataAsOf]; !ok || v <= 0 {
		t.Fatalf("最新日 %s 没有可用数值（v=%v ok=%v）", series.DataAsOf, v, ok)
	}
	if series.Unit != "元/兆瓦时" {
		t.Fatalf("单位 = %q，期望 元/兆瓦时", series.Unit)
	}
	wantLag, wantStale := truthFreshness(series.DataAsOf, time.Now(), priceLocation())
	if series.LagDays != wantLag || series.Stale != wantStale {
		t.Fatalf("滞后/陈旧与 truthFreshness 不一致: got %d/%v want %d/%v",
			series.LagDays, series.Stale, wantLag, wantStale)
	}
	t.Logf("真值最新到 %s（滞后 %d 天，stale=%v），%d 个运行日，单位 %s",
		series.DataAsOf, series.LagDays, series.Stale, len(series.Dates), series.Unit)
}
