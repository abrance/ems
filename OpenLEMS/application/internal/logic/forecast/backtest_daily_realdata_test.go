package forecast

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gogf/gf/v2/database/gdb"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
)

// 真库 + 真预测服务的日频回测：只用来出「实测数字」，CI 上跳过。
//
// 用法：
//
//	GDPM_DB=/tmp/lems-db.sqlite3 go test ./internal/logic/forecast/ -run TestDailyBacktestAgainstRealDB -v -timeout 300s
//
// 为什么必须在真库上跑：回测依赖三件只有真库才有的事 —— 官方指标行的指标名/scope、
// strategy setting 里的预测服务地址、以及真实的历史尾部日期。任何一件变了这里先红。
func TestDailyBacktestAgainstRealDB(t *testing.T) {
	path := os.Getenv("GDPM_DB")
	if path == "" {
		t.Skip("需要 GDPM_DB（ems 的 sqlite 库副本），跳过")
	}
	gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	})
	t.Cleanup(func() { gdb.SetConfig(gdb.Config{}) })

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	bt, err := BacktestPriceDaily(ctx, "ahead", "2026-10-01", "2026-11-13", 3)
	if err != nil {
		t.Fatalf("日频回测失败: %v", err)
	}
	t.Logf("真值最新到 %s（滞后 %d 天，stale=%v），来源 %s",
		bt.DataAsOf, bt.LagDays, bt.Stale, bt.TruthSource)
	t.Logf("目标日 %v，跳过 %v，模型 %s，%d 个点，推理 %.0fms",
		bt.TargetDates, bt.Skipped, bt.Model, len(bt.Points), bt.TimeMs)
	var sumAbs, sumPct float64
	for _, p := range bt.Points {
		abs := p.Predicted - p.Actual
		if abs < 0 {
			abs = -abs
		}
		sumAbs += abs
		if p.Actual != 0 {
			sumPct += abs / p.Actual
		}
		t.Logf("  %s 预测 %.2f 实际 %.2f 误差 %+.2f", p.TargetDate, p.Predicted, p.Actual, p.Predicted-p.Actual)
	}
	if n := len(bt.Points); n > 0 {
		t.Logf("MAE = %.2f 元/MWh，平均绝对百分比误差 = %.2f%%", sumAbs/float64(n), sumPct/float64(n)*100)
	}
	if len(bt.Points) == 0 {
		t.Fatal("一个对比点都没有")
	}
	if bt.DataAsOf == "" || bt.TruthSource == "" {
		t.Fatal("新鲜度/来源字段为空 —— 页面就无从告知数据到哪天")
	}
}
