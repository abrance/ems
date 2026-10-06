package forecast

import (
	"testing"
	"time"

	"s_db/s_db_model"
)

func TestParseGrain(t *testing.T) {
	cases := map[string]string{
		"":           s_db_model.ForecastGrainDay,
		"day":        s_db_model.ForecastGrainDay,
		"daily":      s_db_model.ForecastGrainDay,
		"DAY":        s_db_model.ForecastGrainDay,
		"intraday":   s_db_model.ForecastGrainIntraday,
		"intra-day":  s_db_model.ForecastGrainIntraday,
		"96":         s_db_model.ForecastGrainIntraday,
		" intraday ": s_db_model.ForecastGrainIntraday,
	}
	for in, want := range cases {
		got, err := ParseGrain(in)
		if err != nil {
			t.Errorf("%q 应可解析: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q → %s，期望 %s", in, got, want)
		}
	}
	if _, err := ParseGrain("hourly"); err == nil {
		t.Error("非法粒度应报错")
	}
}

// day 粒度只存日期，intraday 存到分钟 —— 这决定留痕能不能和实际值按时间 join 上
func TestFormatTargetTime(t *testing.T) {
	loc := priceLocation()
	ts := time.Date(2026, 9, 22, 13, 45, 0, 0, loc)

	if got := formatTargetTime(s_db_model.ForecastGrainDay, ts); got != "2026-09-22" {
		t.Errorf("day 粒度应为日期，实际 %q", got)
	}
	if got := formatTargetTime(s_db_model.ForecastGrainIntraday, ts); got != "2026-09-22 13:45" {
		t.Errorf("intraday 应到分钟，实际 %q", got)
	}
}

// CSV 把当天最后一个点写成 24:00，它必须归到次日 00:00，否则序列会出现「同一天 24:00」
// 这种不存在的时刻，也会让后面所有目标时刻偏 15 分钟。
func TestParseSampleClockHandles24(t *testing.T) {
	loc := priceLocation()

	got, err := parseSampleClock("2026-09-21", "24:00", loc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := time.Date(2026, 9, 22, 0, 0, 0, 0, loc); !got.Equal(want) {
		t.Errorf("24:00 应归到次日 00:00，实际 %v", got)
	}

	got, err = parseSampleClock("2026-09-21", "00:15", loc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := time.Date(2026, 9, 21, 0, 15, 0, 0, loc); !got.Equal(want) {
		t.Errorf("00:15 应留在当日，实际 %v", got)
	}

	if _, err := parseSampleClock("2026-09-21", "bad", loc); err == nil {
		t.Error("非法时刻应报错")
	}
}

// CSV 的「一天」写作 00:15…24:00，96 个点里最后一点落在次日 00:00。
// 这就是为什么覆盖天数要数源数据的日期字段，而不是数时间戳。
func TestIntradayDayCountsFromSourceDates(t *testing.T) {
	loc := priceLocation()

	daySet := map[string]bool{}
	var times []time.Time
	// 每个源日期的 96 点都从自己那天的 00:15 起算 —— 这才是 CSV 的写法
	for _, date := range []string{"2026-09-21", "2026-09-22"} {
		base := time.Date(2026, 9, 21, 0, 15, 0, 0, loc)
		if date == "2026-09-22" {
			base = base.AddDate(0, 0, 1)
		}
		for i := 0; i < 96; i++ {
			times = append(times, base.Add(time.Duration(i)*15*time.Minute))
		}
		daySet[date] = true
	}
	if got := len(daySet); got != 2 {
		t.Errorf("源数据日期数应为 2，实际 %d", got)
	}
	// 按时间戳数会得到 3（最后一点跨到次日）—— 这正是不能这么数的原因
	stampDays := map[string]bool{}
	for _, t := range times {
		stampDays[t.Format(priceDateLayout)] = true
	}
	if len(stampDays) != 3 {
		t.Fatalf("本用例前提：时间戳会跨 3 个自然日，实际 %d", len(stampDays))
	}
}

func TestQuantileAt(t *testing.T) {
	m := map[string][]float64{"0.1": {1, 2}, "0.9": {9, 8}}
	if got := quantileAt(m, "0.1", 1); got != 2 {
		t.Errorf("q10[1] 应为 2，实际 %v", got)
	}
	if got := quantileAt(m, "0.9", 0); got != 9 {
		t.Errorf("q90[0] 应为 9，实际 %v", got)
	}
	// 越界与缺键都不能 panic
	if got := quantileAt(m, "0.5", 0); got != 0 {
		t.Errorf("缺键应返回 0，实际 %v", got)
	}
	if got := quantileAt(m, "0.1", 5); got != 0 {
		t.Errorf("越界应返回 0，实际 %v", got)
	}
	if got := quantileAt(nil, "0.1", 0); got != 0 {
		t.Errorf("nil map 应返回 0，实际 %v", got)
	}
}
