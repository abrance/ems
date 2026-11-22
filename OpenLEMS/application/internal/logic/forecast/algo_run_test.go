package forecast

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"s_db/s_db_model"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
)

const algoRunTestDDL = `
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
)`

func setupAlgoRunDB(t *testing.T) context.Context {
	t.Helper()
	path := filepath.Join(t.TempDir(), "algo-run.sqlite3")
	gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	})
	t.Cleanup(func() { gdb.SetConfig(gdb.Config{}) })
	ctx := context.Background()
	if _, err := g.DB().Exec(ctx, algoRunTestDDL); err != nil {
		t.Fatalf("建表: %v", err)
	}
	return ctx
}

func hourPointsFor(date string, base float64) []TPriceForecastPoint {
	day := parseDayTarget(date)
	points := make([]TPriceForecastPoint, 0, 24)
	for h := 0; h < 24; h++ {
		points = append(points, TPriceForecastPoint{
			TargetTime: day.Add(time.Duration(h) * time.Hour),
			Point:      base + float64(h),
			Q10:        base + float64(h) - 20,
			Q50:        base + float64(h),
			Q90:        base + float64(h) + 20,
		})
	}
	return points
}

func TestRunHourForecastRangeWritesBothAlgos(t *testing.T) {
	ctx := setupAlgoRunDB(t)

	load := func(_ context.Context, req PriceRequest) (*TPriceForecastResult, error) {
		if req.TargetDate == "2026-11-11" {
			return nil, errors.New("boom")
		}
		return &TPriceForecastResult{
			Market: req.Market,
			To:     "2026-11-09",
			Model:  "timesfm-test",
			Unit:   "元/兆瓦时",
			Points: hourPointsFor(req.TargetDate, 100),
			GBM: &TPriceForecastGBM{
				Model:  "gbm-test",
				Points: hourPointsFor(req.TargetDate, 110),
			},
		}, nil
	}

	days, written, skipped := runHourForecastRange(ctx, "2026-11-10", "2026-11-12", []string{"ahead"}, load)
	if days != 3 {
		t.Errorf("days = %d, want 3", days)
	}
	if written["ahead"] != 96 {
		t.Errorf("written[ahead] = %d, want 96（2 天 × 24 点 × 2 算法）", written["ahead"])
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], "2026-11-11") {
		t.Errorf("skipped = %v, want 只跳过 2026-11-11", skipped)
	}

	rows, err := g.DB().GetAll(ctx, `SELECT algo, count(*) AS n FROM gd_price_forecast GROUP BY algo ORDER BY algo`)
	if err != nil {
		t.Fatalf("读取留痕: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("算法分组数 = %d, want 2", len(rows))
	}
	for _, row := range rows {
		if row["n"].Int() != 48 {
			t.Errorf("%s 行数 = %d, want 48", row["algo"].String(), row["n"].Int())
		}
	}
}

func TestRunHourForecastRangeKeepsTimesFMWhenGBMMissing(t *testing.T) {
	ctx := setupAlgoRunDB(t)

	load := func(_ context.Context, req PriceRequest) (*TPriceForecastResult, error) {
		return &TPriceForecastResult{
			Market:   req.Market,
			To:       "2026-11-09",
			Model:    "timesfm-test",
			Unit:     "元/兆瓦时",
			Points:   hourPointsFor(req.TargetDate, 100),
			GBMError: "未配置 GBM 预测服务",
		}, nil
	}

	_, written, skipped := runHourForecastRange(ctx, "2026-11-10", "2026-11-10", []string{"ahead"}, load)
	if written["ahead"] != 24 {
		t.Errorf("written[ahead] = %d, want 24", written["ahead"])
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], "GBM 不可用") {
		t.Errorf("skipped = %v, want 记录 GBM 不可用", skipped)
	}

	n, err := g.DB().GetValue(ctx, `SELECT count(*) FROM gd_price_forecast WHERE algo = ?`, s_db_model.ForecastAlgoTimesFM)
	if err != nil {
		t.Fatal(err)
	}
	if n.Int() != 24 {
		t.Errorf("timesfm 行数 = %d, want 24", n.Int())
	}
}
