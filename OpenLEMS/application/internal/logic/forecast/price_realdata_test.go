package forecast

import (
	"context"
	"testing"
	"time"

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

// 现场事故回归（2026-10-09 查证）：CSV 里最近几天只有电查查预测占位（999）时，
// 回测把它们当「实际值」用 —— 「预测复盘」页显示的准确度其实是「TimesFM 与电查查谁准」。
// 需要本地有 gd_metric.csv 缓存与可达的预测服务；缺一就跳过（CI 上就是跳过）。
func TestBacktestRealDataUsesOnlyActualValues(t *testing.T) {
	ctx := context.Background()
	if !gdmetric.LocalDataReady() {
		t.Skip("本地没有 gd_metric.csv 缓存，跳过")
	}
	snap, err := gdmetric.GetSnapshot(ctx, "", "", "")
	if err != nil || snap == nil || snap.LatestRealDate == "" {
		t.Skipf("拿不到实际值最新日，跳过: %v", err)
	}
	loc := priceLocation()
	lastReal, err := time.ParseInLocation(priceDateLayout, snap.LatestRealDate, loc)
	if err != nil {
		t.Skipf("解析 %s 失败，跳过", snap.LatestRealDate)
	}

	// ① 只有占位预测的区间：必须明确拒绝，而不是拿预测当实际值算出一堆数字
	if snap.LatestDate > snap.LatestRealDate {
		_, err := BacktestPriceIntraday(ctx, "ahead", lastReal.AddDate(0, 0, 1).Format(priceDateLayout),
			snap.LatestDate, 3)
		if err == nil {
			t.Errorf("区间 %s ~ %s 只有电查查预测占位（无实际值），回测应当报错",
				lastReal.AddDate(0, 0, 1).Format(priceDateLayout), snap.LatestDate)
		} else {
			t.Logf("占位区间被拒绝: %v", err)
		}
	}

	// ② 最近一个「实际值完整」的日期：跑得出来，而且每一点的「实际值」都必须能在实际值序列里对上号。
	// 注意 latestRealDate 只保证那天**有**实际值，不保证 96 点齐全（10-02 就只有 92 点），
	// 所以候选日要用同一套规则挑。
	window := lastReal.AddDate(0, 0, -30).Format(priceDateLayout)
	series, err := gdmetric.GetRealSeries(ctx, window, snap.LatestRealDate, gdmetric.KindDayAhead)
	if err != nil {
		t.Fatalf("读实际值序列失败: %v", err)
	}
	candidates, _ := backtestGroundTruth(series)
	if len(candidates) == 0 {
		t.Skipf("%s ~ %s 内没有 96 点齐全的实际值日，跳过", window, snap.LatestRealDate)
	}
	date := candidates[len(candidates)-1]
	res, err := BacktestPriceIntraday(ctx, "ahead", date, date, 30)
	if err != nil {
		t.Fatalf("回测 %s 失败: %v", date, err)
	}
	if len(res.Points) == 0 {
		t.Fatalf("回测 %s 返回 0 个对比点", date)
	}
	daySeries, err := gdmetric.GetRealSeries(ctx, date, date, gdmetric.KindDayAhead)
	if err != nil {
		t.Fatalf("读实际值序列失败: %v", err)
	}
	byTime := make(map[string]float64, len(daySeries))
	for _, s := range daySeries {
		ts, err := parseSampleClock(s.Date, s.Time, loc)
		if err != nil {
			continue
		}
		byTime[sampleKey(ts)] = s.Value
	}
	for _, p := range res.Points {
		want, ok := byTime[sampleKey(p.TargetTime)]
		if !ok {
			t.Errorf("%s 配到的实际值不在实际值序列里（配上了占位行？）", p.TargetTime)
			continue
		}
		if want != p.Actual {
			t.Errorf("%s 实际值 = %v，实际值序列是 %v", p.TargetTime, p.Actual, want)
		}
	}
	t.Logf("%s：%d 点（无实际值丢弃 %d 点），模型 %s，MAE≈%.1f",
		date, len(res.Points), res.ActualMissing, res.Model, meanAbsErr(res.Points))
}

func meanAbsErr(points []SPriceBacktestPoint) float64 {
	if len(points) == 0 {
		return 0
	}
	sum := 0.0
	for _, p := range points {
		d := p.Predicted - p.Actual
		if d < 0 {
			d = -d
		}
		sum += d
	}
	return sum / float64(len(points))
}

// 实时电价走多变量输入（实时价 + 日前价 + 系统负荷），日前保持单变量。
//
// 依据：2026-09-08 ~ 10-01 共 24 天真实实际值（sourceId=9）离线对照，
// 实时 MAE 96.4 → 89.5 元/MWh（标幺 0.191 → 0.177）；日前加同样通道持平或更差。
// 这条用例同时验证「二维请求 + 二维响应取第一个通道」在真实服务上跑得通。
// 需要本地有 gd_metric.csv 缓存与可达的预测服务；缺一就跳过。
func TestLoadPriceIntradayChannels(t *testing.T) {
	ctx := context.Background()
	if !gdmetric.LocalDataReady() {
		t.Skip("本地没有 gd_metric.csv 缓存，跳过")
	}

	real, err := LoadPriceIntraday(ctx, "real", 30, 1, nil)
	if err != nil {
		t.Fatalf("实时电价预测失败（多变量请求可能没被服务端接受）: %v", err)
	}
	if real.InputChannels != 3 {
		t.Errorf("实时电价通道数 = %d, want 3（实时价 + 日前价 + 负荷）", real.InputChannels)
	}
	if len(real.Points) == 0 || real.ContextPoints == 0 {
		t.Fatalf("实时预测结果为空: %+v", real)
	}

	ahead, err := LoadPriceIntraday(ctx, "ahead", 30, 1, nil)
	if err != nil {
		t.Fatalf("日前电价预测失败: %v", err)
	}
	if ahead.InputChannels != 1 {
		t.Errorf("日前电价通道数 = %d, want 1（实测加协变量无增益）", ahead.InputChannels)
	}
	t.Logf("实时：%d 通道 / %d 点上下文 / %d 点预测，模型 %s；日前：%d 通道",
		real.InputChannels, real.ContextPoints, len(real.Points), real.Model, ahead.InputChannels)
}
