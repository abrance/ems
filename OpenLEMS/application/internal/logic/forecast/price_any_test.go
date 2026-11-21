package forecast

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"s_db/s_db_model"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

func TestParseGrain(t *testing.T) {
	cases := map[string]string{
		"":           s_db_model.ForecastGrainDay,
		"day":        s_db_model.ForecastGrainDay,
		"daily":      s_db_model.ForecastGrainDay,
		"DAY":        s_db_model.ForecastGrainDay,
		"hour":       s_db_model.ForecastGrainHour,
		"hourly":     s_db_model.ForecastGrainHour,
		"24":         s_db_model.ForecastGrainHour,
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
	if _, err := ParseGrain("weekly"); err == nil {
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
	// hour 与 intraday 一样到分钟，整点即 HH:00
	if got := formatTargetTime(s_db_model.ForecastGrainHour, time.Date(2026, 9, 22, 13, 0, 0, 0, loc)); got != "2026-09-22 13:00" {
		t.Errorf("hour 应到分钟，实际 %q", got)
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

// ---------------------------------------------------------------------------
// 留痕只写「目标日在生成日之后」的点
//
// 线上形态（2026-11-13 快照）：官方日频 T+2，历史止于今天-2，nextDates 从最后一条
// 历史日 +1 起算 → 前几个预测日的 target_time ≤ generated_on。这些行会被复盘页的
// IsBackfilledForecast 全部剔除，等于白写；更糟的是它们混在留痕里，让「当时预测」
// 与「事后补算」两种语义在同一张表里分不开。
// ---------------------------------------------------------------------------

// 建一张空的 gd_price_forecast，供留痕用例共用。
func setupForecastTable(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "price-forecast.sqlite3")
	gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	})
	t.Cleanup(func() { gdb.SetConfig(gdb.Config{}) })

	if _, err := g.DB().Exec(context.Background(), `
		CREATE TABLE gd_price_forecast (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			generated_on TEXT NOT NULL,
			market TEXT NOT NULL,
			grain TEXT NOT NULL,
			algo VARCHAR(16) NOT NULL DEFAULT 'timesfm',
			target_time TEXT NOT NULL,
			point REAL NOT NULL DEFAULT 0,
			q10 REAL NOT NULL DEFAULT 0,
			q50 REAL NOT NULL DEFAULT 0,
			q90 REAL NOT NULL DEFAULT 0,
			model TEXT NOT NULL DEFAULT '',
			unit TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (generated_on, market, grain, algo, target_time)
		)`); err != nil {
		t.Fatalf("建表: %v", err)
	}
}

// 读回留痕的 (generated_on, target_time) 对，按 target_time 升序。
func readForecastRows(t *testing.T) [][2]string {
	t.Helper()
	rows, err := g.DB().GetAll(context.Background(),
		`SELECT generated_on, target_time FROM gd_price_forecast ORDER BY target_time`)
	if err != nil {
		t.Fatalf("读取留痕: %v", err)
	}
	out := make([][2]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, [2]string{r["generated_on"].String(), r["target_time"].String()})
	}
	return out
}

// 线上真实形态：历史止于今天-2（官方 T+2 滞后），day 粒度预测从今天-1 起算。
// 前两个点（今天-1、今天）的 target ≤ generated_on，必须被跳过；
// 剩下的每一条都要能被复盘页当「当时预测」用（IsBackfilledForecast == false）。
func TestRecordForecastPointsSkipsTargetsNotAfterGeneratedOn(t *testing.T) {
	setupForecastTable(t)
	ctx := context.Background()

	// generated_on = 2026-11-13，历史止于 2026-11-11
	generatedOn := "2026-11-13"
	res := &TPriceForecastResult{
		Market:      "ahead",
		Grain:       s_db_model.ForecastGrainDay,
		Model:       "timesfm-3.0",
		Unit:        "元/MWh",
		GeneratedOn: generatedOn,
		Points: []TPriceForecastPoint{
			{TargetTime: parseDayTarget("2026-11-12"), Point: 380.1}, // 昨天，剔除
			{TargetTime: parseDayTarget("2026-11-13"), Point: 385.2}, // 今天，剔除
			{TargetTime: parseDayTarget("2026-11-14"), Point: 401.3},
			{TargetTime: parseDayTarget("2026-11-15"), Point: 412.7},
		},
	}

	n := recordForecastPoints(ctx, res)
	if n != 2 {
		t.Errorf("写入条数 = %d，want 2（只写 target > generated_on 的点）", n)
	}

	rows := readForecastRows(t)
	if len(rows) != 2 {
		t.Fatalf("库里行数 = %d，want 2；实际 %v", len(rows), rows)
	}
	for _, r := range rows {
		generatedOnRow, target := r[0], r[1]
		if target <= generatedOnRow {
			t.Errorf("写入了 target_time=%s ≤ generated_on=%s 的留痕行", target, generatedOnRow)
		}
		if IsBackfilledForecast(generatedOnRow, target) {
			t.Errorf("留痕会被复盘剔除: generated_on=%s target_time=%s", generatedOnRow, target)
		}
	}
	// 顺序与内容都要对：剔除的是前两条，留下的正是 11-14 / 11-15
	if rows[0][1] != "2026-11-14" || rows[1][1] != "2026-11-15" {
		t.Errorf("留下的目标日 = %v，want [2026-11-14 2026-11-15]", rows)
	}
}

// 反向守卫：历史止于今天（预测日全在未来）时一条都不能少。
// 这条防「过滤写反方向」——把 > 写成 <、或按 >= 过滤未来点。
func TestRecordForecastPointsKeepsAllFutureTargets(t *testing.T) {
	setupForecastTable(t)
	ctx := context.Background()

	generatedOn := "2026-11-13"
	res := &TPriceForecastResult{
		Market:      "ahead",
		Grain:       s_db_model.ForecastGrainDay,
		Model:       "timesfm-3.0",
		Unit:        "元/MWh",
		GeneratedOn: generatedOn,
		Points: []TPriceForecastPoint{
			{TargetTime: parseDayTarget("2026-11-14"), Point: 401.3},
			{TargetTime: parseDayTarget("2026-11-15"), Point: 412.7},
			{TargetTime: parseDayTarget("2026-11-16"), Point: 398.4},
		},
	}

	if n := recordForecastPoints(ctx, res); n != 3 {
		t.Errorf("写入条数 = %d，want 3（全在未来，不应被过滤）", n)
	}
	rows := readForecastRows(t)
	if len(rows) != 3 {
		t.Fatalf("库里行数 = %d，want 3；实际 %v", len(rows), rows)
	}
	want := []string{"2026-11-14", "2026-11-15", "2026-11-16"}
	for i, w := range want {
		if rows[i][1] != w {
			t.Errorf("第 %d 条 target = %s，want %s", i, rows[i][1], w)
		}
	}
}

// intraday 的目标时刻带时分，但 generated_on 只有日期精度（当天生成不记录时刻），
// 所以判据和 day 粒度一样只比日期部分，与复盘页 IsBackfilledForecast 同源。
// 后果：当天生成的 intraday 点即使时分已过，也仍算「当时预测」留痕 —— 复盘页同样不剔它。
// 要做到分钟级过滤，generated_on 必须带时间戳（新列），不在本次范围。
func TestRecordForecastPointsIntradayKeepsSameDayButDropsStaleDays(t *testing.T) {
	setupForecastTable(t)
	ctx := context.Background()

	loc := priceLocation()
	generatedOn := "2026-11-13"
	res := &TPriceForecastResult{
		Market:      "real",
		Grain:       s_db_model.ForecastGrainIntraday,
		Model:       "timesfm-3.0",
		Unit:        "元/MWh",
		GeneratedOn: generatedOn,
		Points: []TPriceForecastPoint{
			{TargetTime: time.Date(2026, 11, 11, 23, 45, 0, 0, loc), Point: 1}, // 早于生成日，剔
			{TargetTime: time.Date(2026, 11, 13, 0, 15, 0, 0, loc), Point: 2},  // 当日，留
			{TargetTime: time.Date(2026, 11, 13, 23, 45, 0, 0, loc), Point: 3}, // 当日，留
			{TargetTime: time.Date(2026, 11, 14, 0, 0, 0, 0, loc), Point: 4},   // 次日，留
		},
	}

	if n := recordForecastPoints(ctx, res); n != 3 {
		t.Errorf("写入条数 = %d，want 3（仅剔除早于生成日的那条）", n)
	}
	rows := readForecastRows(t)
	if len(rows) != 3 {
		t.Fatalf("留痕 = %v，want 3 条", rows)
	}
	for _, r := range rows {
		if IsBackfilledForecast(r[0], r[1]) {
			t.Errorf("留痕会被复盘剔除: generated_on=%s target_time=%s", r[0], r[1])
		}
	}
}
