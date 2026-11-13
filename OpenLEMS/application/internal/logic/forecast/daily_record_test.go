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

func dailyRecordTestNow() time.Time {
	return time.Date(2026, 11, 13, 15, 0, 0, 0, priceLocation())
}

// 目标日=生成日次日，且这一行能通过复盘「当时预测」腿的回填过滤。
func TestBuildDailyRecordRowPicksTomorrowAndPassesReviewFilter(t *testing.T) {
	now := dailyRecordTestNow()
	generatedOn := now.Format(priceDateLayout)
	targetDate := now.AddDate(0, 0, 1).Format(priceDateLayout)

	res := &TPriceForecastResult{
		Market: "ahead",
		Grain:  s_db_model.ForecastGrainDay,
		Model:  "timesfm-3.0",
		Unit:   "元/MWh",
		To:     "2026-11-11",
		Points: []TPriceForecastPoint{
			{TargetTime: parseDayTarget("2026-11-12"), Point: 387.27},
			{
				TargetTime: parseDayTarget(targetDate),
				Point:      430.82,
				Q10:        400.1,
				Q50:        430.82,
				Q90:        460.5,
			},
			{TargetTime: parseDayTarget("2026-11-15"), Point: 440.43},
		},
	}

	row, err := BuildDailyRecordRow(res, "ahead", generatedOn, targetDate)
	if err != nil {
		t.Fatalf("BuildDailyRecordRow: %v", err)
	}
	if row.GeneratedOn != generatedOn {
		t.Errorf("generated_on = %q, want %q", row.GeneratedOn, generatedOn)
	}
	if row.TargetTime != targetDate {
		t.Errorf("target_time = %q, want %q（次日）", row.TargetTime, targetDate)
	}
	if row.Grain != s_db_model.ForecastGrainDay {
		t.Errorf("grain = %q, want day", row.Grain)
	}
	if !(row.GeneratedOn < row.TargetTime) {
		t.Errorf("必须满足 generated_on < target_time，实际 %q / %q", row.GeneratedOn, row.TargetTime)
	}
	// 复盘页的 recorded 腿用的就是这条判断：false 才不会被当回填剔除。
	if IsBackfilledForecast(row.GeneratedOn, row.TargetTime) {
		t.Errorf("留痕被复盘当成回填剔除：generated_on=%q target_time=%q", row.GeneratedOn, row.TargetTime)
	}
	if row.Point != 430.82 || row.Q90 != 460.5 {
		t.Errorf("取错预测点或分位: %+v", row)
	}
	if row.Market != "ahead" {
		t.Errorf("market = %q", row.Market)
	}
}

func TestBuildDailyRecordRowSkipsMissingOrZero(t *testing.T) {
	now := dailyRecordTestNow()
	generatedOn := now.Format(priceDateLayout)
	targetDate := now.AddDate(0, 0, 1).Format(priceDateLayout)

	base := func(points []TPriceForecastPoint) *TPriceForecastResult {
		return &TPriceForecastResult{
			Market: "ahead",
			Grain:  s_db_model.ForecastGrainDay,
			Unit:   "元/MWh",
			To:     "2026-11-11",
			Points: points,
		}
	}

	// 目标日不在预测范围（历史数据太旧）→ 跳过，不补 0。
	if _, err := BuildDailyRecordRow(base([]TPriceForecastPoint{
		{TargetTime: parseDayTarget("2026-11-12"), Point: 387.27},
	}), "ahead", generatedOn, targetDate); err == nil {
		t.Fatal("目标日缺失时应返回错误，不能写一条假记录")
	}

	// 目标日存在但值为 0（缺值占位）→ 跳过，不写 0。
	if _, err := BuildDailyRecordRow(base([]TPriceForecastPoint{
		{TargetTime: parseDayTarget(targetDate), Point: 0},
	}), "ahead", generatedOn, targetDate); err == nil {
		t.Fatal("目标日预测值为 0 时应跳过，不能写 0")
	}

	// 生成日不早于目标日 → 会被复盘当回填，直接拒绝。
	if _, err := BuildDailyRecordRow(base([]TPriceForecastPoint{
		{TargetTime: parseDayTarget(targetDate), Point: 430.82},
	}), "ahead", targetDate, targetDate); err == nil {
		t.Fatal("generated_on 不早于 target_time 时应拒绝")
	}
}

// 跨零点：23:59:5x 触发，loadDaily 里取时刻时已经过了 00:00。
// 生成日必须用「真正算出结果」的那天（loadDaily 打的戳），不是触发日 ——
// 否则留痕会被标成早一天生成，凭空多出一天的提前量，复盘配对也跟着错位。
func TestRunDailyRecordUsesComputeDateAcrossMidnight(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "daily-record-midnight.sqlite3")
	gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	})
	t.Cleanup(func() { gdb.SetConfig(gdb.Config{}) })

	if _, err := g.DB().Exec(ctx, `
		CREATE TABLE gd_price_forecast (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			generated_on TEXT NOT NULL,
			market TEXT NOT NULL,
			grain TEXT NOT NULL,
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
			UNIQUE (generated_on, market, grain, target_time)
		)`); err != nil {
		t.Fatalf("建表: %v", err)
	}

	// 触发时刻：D 日 23:59:58
	trigger := time.Date(2026, 11, 13, 23, 59, 58, 0, priceLocation())
	// loadDaily 内部取时刻：已跨到 D+1 00:00:03，所以 res.GeneratedOn = D+1
	load := func(_ context.Context, req PriceRequest) (*TPriceForecastResult, error) {
		return &TPriceForecastResult{
			Market:      req.Market,
			Grain:       s_db_model.ForecastGrainDay,
			Model:       "timesfm-3.0",
			Unit:        "元/MWh",
			To:          "2026-11-11",
			GeneratedOn: "2026-11-14",
			Points: []TPriceForecastPoint{
				{TargetTime: parseDayTarget("2026-11-14"), Point: 401.3},
				{TargetTime: parseDayTarget("2026-11-15"), Point: 412.7},
			},
		}, nil
	}

	runDailyRecord(ctx, trigger, load)

	rows, err := g.DB().GetAll(ctx, `SELECT generated_on, target_time FROM gd_price_forecast`)
	if err != nil {
		t.Fatalf("读取留痕: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("留痕行数 = %d，want 2（每个市场一行）", len(rows))
	}
	for _, row := range rows {
		generatedOn := row["generated_on"].String()
		target := row["target_time"].String()
		if generatedOn != "2026-11-14" {
			t.Errorf("generated_on = %s，want 2026-11-14（算出结果的时刻，不是触发日 2026-11-13）", generatedOn)
		}
		if generatedOn >= target {
			t.Errorf("写入了 target_time=%s ≤ generated_on=%s 的行", target, generatedOn)
		}
		if IsBackfilledForecast(generatedOn, target) {
			t.Errorf("留痕会被复盘剔除: generated_on=%s target_time=%s", generatedOn, target)
		}
	}
}

// 同一目标日执行两次，库里仍只有一行：唯一键 + UpsertBatch 覆盖，不堆重复。
func TestRunDailyRecordIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "daily-record.sqlite3")
	gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	})
	t.Cleanup(func() { gdb.SetConfig(gdb.Config{}) })

	if _, err := g.DB().Exec(ctx, `
		CREATE TABLE gd_price_forecast (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			generated_on TEXT NOT NULL,
			market TEXT NOT NULL,
			grain TEXT NOT NULL,
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
			UNIQUE (generated_on, market, grain, target_time)
		)`); err != nil {
		t.Fatalf("建表: %v", err)
	}

	now := dailyRecordTestNow()
	targetDate := now.AddDate(0, 0, 1).Format(priceDateLayout)
	load := func(_ context.Context, req PriceRequest) (*TPriceForecastResult, error) {
		return &TPriceForecastResult{
			Market: req.Market,
			Grain:  s_db_model.ForecastGrainDay,
			Model:  "timesfm-3.0",
			Unit:   "元/MWh",
			To:     "2026-11-11",
			Points: []TPriceForecastPoint{
				{TargetTime: parseDayTarget("2026-11-12"), Point: 387.27},
				{TargetTime: parseDayTarget(targetDate), Point: 430.82, Q10: 400.1, Q50: 430.82, Q90: 460.5},
			},
		}, nil
	}

	first := runDailyRecord(ctx, now, load)
	second := runDailyRecord(ctx, now, load)
	for _, market := range dailyRecordMarkets {
		if first[market] != 1 || second[market] != 1 {
			t.Errorf("%s 两次执行写入条数 = %d / %d，want 1 / 1", market, first[market], second[market])
		}
	}

	total, err := g.DB().GetValue(ctx, `SELECT COUNT(*) FROM gd_price_forecast`)
	if err != nil {
		t.Fatalf("统计行数: %v", err)
	}
	if total.Int() != 2 {
		t.Errorf("两次执行后总行数 = %d，want 2（每个市场一行）", total.Int())
	}

	for _, market := range dailyRecordMarkets {
		got, err := g.DB().GetValue(ctx,
			`SELECT COUNT(*) FROM gd_price_forecast WHERE market=? AND grain=? AND target_time=?`,
			market, s_db_model.ForecastGrainDay, targetDate)
		if err != nil {
			t.Fatalf("统计 %s 行数: %v", market, err)
		}
		if got.Int() != 1 {
			t.Errorf("%s 同一目标日行数 = %d，want 1", market, got.Int())
		}
	}

	// 库里每一行的生成日都早于目标日 —— 复盘不会把它们当回填剔除。
	rows, err := g.DB().GetAll(ctx, `SELECT generated_on, target_time FROM gd_price_forecast`)
	if err != nil {
		t.Fatalf("读取留痕: %v", err)
	}
	for _, row := range rows {
		generatedOn := row["generated_on"].String()
		target := row["target_time"].String()
		if IsBackfilledForecast(generatedOn, target) {
			t.Errorf("留痕被复盘剔除了: generated_on=%s target_time=%s", generatedOn, target)
		}
	}
}

// 自然日对齐：nextDailyRun 必须返回 Asia/Shanghai 的下一个 00:05，
// 不依赖进程启动时刻（旧实现固定 24h，节奏 = 重启频率，11-09/11-10 缺、11-13 双写）。
func TestNextDailyRunAlignsToNaturalDay(t *testing.T) {
	loc := priceLocation()
	at := func(y int, mo time.Month, d, h, mi, s int) time.Time {
		return time.Date(y, mo, d, h, mi, s, 0, loc)
	}

	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"普通日", at(2026, 11, 13, 15, 0, 0), at(2026, 11, 14, 0, 5, 0)},
		{"跨月", at(2026, 10, 31, 23, 59, 59), at(2026, 11, 1, 0, 5, 0)},
		{"跨年", at(2026, 12, 31, 23, 59, 59), at(2027, 1, 1, 0, 5, 0)},
		{"闰年 2 月 28 日", at(2028, 2, 28, 23, 59, 59), at(2028, 2, 29, 0, 5, 0)},
		{"闰年 2 月 29 日", at(2028, 2, 29, 23, 59, 59), at(2028, 3, 1, 0, 5, 0)},
		{"23:59:58 归次日不归当天", at(2026, 11, 13, 23, 59, 58), at(2026, 11, 14, 0, 5, 0)},
		{"恰好 00:05:00 归次日", at(2026, 11, 14, 0, 5, 0), at(2026, 11, 15, 0, 5, 0)},
		{"00:04:59 归当天", at(2026, 11, 14, 0, 4, 59), at(2026, 11, 14, 0, 5, 0)},
		// UTC 16:30 = CST 次日 00:30：必须按 CST 的自然日算，不是 UTC/Local 的「自然日」。
		{"UTC 输入按 CST 自然日算", time.Date(2026, 11, 13, 16, 30, 0, 0, time.UTC), at(2026, 11, 15, 0, 5, 0)},
	}

	for _, c := range cases {
		got := nextDailyRun(c.now)
		if !got.Equal(c.want) {
			t.Errorf("%s: nextDailyRun(%s) = %s，want %s",
				c.name, c.now.Format(time.RFC3339), got.Format(time.RFC3339), c.want.Format(time.RFC3339))
		}
		// 用 CST 时钟读数比对，不硬绑 Location 字符串（无 tzdata 环境会退化成 FixedZone）。
		if gh, wh := got.In(loc).Format("2006-01-02 15:04:05"), c.want.In(loc).Format("2006-01-02 15:04:05"); gh != wh {
			t.Errorf("%s: CST 读数 = %s，want %s", c.name, gh, wh)
		}
		if !got.After(c.now) {
			t.Errorf("%s: 返回值 %s 不晚于 now %s", c.name, got, c.now)
		}
	}
}

// 间隔由 nextDailyRun 推出来：同一时刻反复算，延迟落在 (0, 24h] 内且每天恰好一次。
func TestDailyRecordDelayMatchesNextDailyRun(t *testing.T) {
	loc := priceLocation()
	now := time.Date(2026, 11, 13, 15, 0, 0, 0, loc)
	want := nextDailyRun(now).Sub(now)
	if got := dailyRecordDelay(now); got != want {
		t.Errorf("dailyRecordDelay = %s，want %s", got, want)
	}
	// 15:00 → 次日 00:05 = 9h5m。钉绝对值，固定 24h 的旧行为在这里必红。
	if want != 9*time.Hour+5*time.Minute {
		t.Errorf("15:00 的延迟 = %s，want 9h5m（次日 00:05）", want)
	}
	// 一整天里每个整点算一次，延迟都必须是正的、不超过 24h（旧实现固定 24h 无法满足 00:05 对齐）。
	for h := 0; h < 24; h++ {
		n := time.Date(2026, 11, 13, h, 0, 0, 0, loc)
		if d := dailyRecordDelay(n); d <= 0 || d > 24*time.Hour {
			t.Errorf("%02d:00 的延迟 = %s，不在 (0, 24h] 内", h, d)
		}
	}
}

// 启动补跑保留：首次等待仍是 90s，且 Start 用的就是这个值。
func TestDailyRecordFirstWaitIsStartupDelay(t *testing.T) {
	if got := dailyRecordFirstWait(); got != 90*time.Second {
		t.Errorf("dailyRecordFirstWait = %s，want 90s", got)
	}
	if dailyRecordFirstWait() != dailyRecordStartupDelay {
		t.Errorf("首次等待与 dailyRecordStartupDelay 不一致")
	}
}
