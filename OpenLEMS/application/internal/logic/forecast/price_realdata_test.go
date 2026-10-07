package forecast

import (
	"context"
	"testing"

	"application/internal/logic/gdmetric"
)

// 端到端回归：这条路径曾经整体失败 ——
// 「15 分钟日前电价数据不足（0 点，至少需要一天 96 点）」，
// 因为 CSV 只到 08-31，而代码拿「今天」(10-07) 当锚点，取的窗口全空。
//
// 需要本地有 gd_metric.csv 缓存与可达的预测服务；缺一就跳过（CI 上就是跳过）。
func TestLoadPriceIntradayRealData(t *testing.T) {
	ctx := context.Background()
	if !gdmetric.LocalDataReady() {
		t.Skip("本地没有 gd_metric.csv 缓存，跳过（CI 上也会跳过，不为跑用例下载 37MB）")
	}

	res, err := LoadPriceIntraday(ctx, "ahead", 30, 5, nil)
	if err != nil {
		t.Fatalf("96 点电价预测失败（正是这个 bug 的症状）: %v", err)
	}
	if len(res.Points) == 0 {
		t.Fatal("返回了 0 个预测点")
	}
	if res.CoveredDays < 1 {
		t.Errorf("源数据覆盖天数 = %d，期望至少 1 天", res.CoveredDays)
	}

	// 预测必须落在「数据最后一天」之后：落在今天附近就说明锚点没收回去
	snap, err := gdmetric.GetSnapshot(ctx, "", "", "")
	if err == nil && snap != nil && snap.LatestDate != "" {
		first := res.Points[0].TargetTime.Format(priceDateLayout)
		last := res.Points[len(res.Points)-1].TargetTime.Format(priceDateLayout)
		if first <= snap.LatestDate {
			t.Errorf("首个预测目标日 %s 不在数据最后一天 %s 之后 → 锚点没收回去", first, snap.LatestDate)
		}
		t.Logf("数据最新 %s → 预测 %s ~ %s（%d 点，覆盖 %d 天，耗时 %.0fms）",
			snap.LatestDate, first, last, len(res.Points), res.CoveredDays, res.TimeMs)
	}
}
