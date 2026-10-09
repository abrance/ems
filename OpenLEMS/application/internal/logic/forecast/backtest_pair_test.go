package forecast

import (
	"testing"
	"time"

	"application/internal/logic/gdmetric"
)

// 回归用例：预测窗口与实际日错开时，必须按**时刻**配对。
//
// 现场事故（2026-10-09 查证）：10-02 的 CSV 断在 23:00（少 4 点），
// 预测起点 = 历史最后一点 +15min = 10-02 23:15，而实际序列是 10-03 的 00:15…24:00。
// 旧代码按下标 actual[i] 配对，于是每个预测点都比对到了 1 小时之后的实际值，
// 误差里混进了「拿 00 点的价去比 01 点的价」这种完全假的偏差。
func TestPairByTimestampSkipsOffsetPoints(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 10, 2, 23, 15, 0, 0, loc)

	// 实际值：10-03 的 00:15 … 01:15，价格逐小时不同，错配立刻看得出来
	actual := map[string]float64{}
	for i := 0; i < 5; i++ {
		ts := time.Date(2026, 10, 3, 0, 15, 0, 0, loc).Add(time.Duration(i) * 15 * time.Minute)
		actual[sampleKey(ts)] = float64(100 * (i + 1)) // 00:15=100 … 01:15=500
	}

	// 预测：从 10-02 23:15 起 8 点，值随时刻递增（错配一眼能看出来）
	preds := make([]TPriceForecastPoint, 0, 8)
	for i := 0; i < 8; i++ {
		preds = append(preds, TPriceForecastPoint{
			TargetTime: start.Add(time.Duration(i) * 15 * time.Minute),
			Point:      500 + float64(i*10),
		})
	}

	paired, missing := pairByTimestamp(preds, actual)
	if missing != 4 { // 23:15 / 23:30 / 23:45 / 00:00 这四点没有实际值
		t.Errorf("配不上的点数 = %d, want 4", missing)
	}
	if len(paired) != 4 {
		t.Fatalf("配对数 = %d, want 4", len(paired))
	}
	// 每一点都必须与同一时刻的实际值比：00:15→100, 00:30→200, …
	for i, p := range paired {
		if got := p.TargetTime.Format("01-02 15:04"); got != []string{
			"10-03 00:15", "10-03 00:30", "10-03 00:45", "10-03 01:00",
		}[i] {
			t.Fatalf("第 %d 点时刻 = %s", i, got)
		}
		if p.Actual != float64(100*(i+1)) {
			t.Errorf("第 %d 点配到了 %v，说明还是按下标对齐", i, p.Actual)
		}
		if p.Predicted != 500+float64(i*10+40) {
			t.Errorf("第 %d 点预测值 = %v，说明预测时刻被整体挪位", i, p.Predicted)
		}
	}
}

// 24:00 归次日 00:00：实际序列的写法与实际日边界必须与预测时间轴一致。
func TestPairByTimestampHandlesMidnight(t *testing.T) {
	loc := time.UTC
	// CSV 里 10-05 的最后一点写作 24:00，归到 10-06 00:00
	ts, err := parseSampleClock("2026-10-05", "24:00", loc)
	if err != nil {
		t.Fatal(err)
	}
	if ts.Format("2006-01-02 15:04") != "2026-10-06 00:00" {
		t.Fatalf("24:00 应归次日 00:00, got %s", ts)
	}
	actual := map[string]float64{sampleKey(ts): 88}
	paired, missing := pairByTimestamp([]TPriceForecastPoint{
		{TargetTime: time.Date(2026, 10, 6, 0, 0, 0, 0, loc), Point: 90},
		{TargetTime: time.Date(2026, 10, 6, 0, 15, 0, 0, loc), Point: 91},
	}, actual)
	if len(paired) != 1 || missing != 1 || paired[0].Actual != 88 {
		t.Fatalf("跨零点配对错: paired=%+v missing=%d", paired, missing)
	}
}

// 回归用例：CSV 里最近几天只有电查查预测占位（999）时，那些天**不能**当回测目标日。
//
// 现场事故（2026-10-09 查证）：10-03 ~ 10-06 的日前/实时电价在 CSV 里全是 999
// （广东的实际出清价还没出），回测却把它们当「实际值」，
// 于是「预测复盘」页显示的准确度其实是「TimesFM 与电查查两个模型差多少」。
func TestBacktestGroundTruthIgnoresPlaceholder(t *testing.T) {
	loc := time.UTC
	day := func(date string, n int, source int) []gdmetric.TimedSample {
		out := make([]gdmetric.TimedSample, 0, n)
		base, err := time.ParseInLocation(priceDateLayout, date, loc)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < n; i++ {
			out = append(out, gdmetric.TimedSample{
				Date:     date,
				Time:     base.Add(time.Duration(i+1) * 15 * time.Minute).Format("15:04"),
				Value:    float64(100 + i),
				SourceID: source,
			})
		}
		return out
	}

	series := append(
		append(day("2026-10-01", 96, gdmetric.SourceExchange),
			day("2026-10-02", 92, gdmetric.SourceExchange)...),
		day("2026-10-03", 96, gdmetric.SourceForecastPlaceholder)...,
	)
	series = append(series, day("2026-10-04", 96, gdmetric.SourceForecastPlaceholder)...)

	candidates, byTime := backtestGroundTruth(series)
	if len(candidates) != 1 || candidates[0] != "2026-10-01" {
		t.Fatalf("候选日 = %v, want [2026-10-01]（占位日与缺点日都不算）", candidates)
	}
	// 占位时刻的值不能进配对表
	if _, ok := byTime["2026-10-03 00:15"]; ok {
		t.Error("10-03 是电查查预测占位，不该进基准真值表")
	}
	if v, ok := byTime["2026-10-01 00:15"]; !ok || v != 100 {
		t.Errorf("实际值没进配对表: %v %v", v, ok)
	}
}
