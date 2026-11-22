package forecast

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	v1 "application/api/forecast/v1"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
)

const algoReviewTestDDLPrice = `
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

const algoReviewTestDDLHome = `
CREATE TABLE gd_home_24h (
	price_date VARCHAR(10) NOT NULL,
	hour INTEGER NOT NULL,
	power_mw REAL NOT NULL DEFAULT 0,
	ahead_price REAL NOT NULL DEFAULT 0,
	realtime_price REAL NOT NULL DEFAULT 0,
	source VARCHAR(64) NOT NULL DEFAULT '',
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE (price_date, hour)
)`

func setupAlgoReviewDB(t *testing.T) context.Context {
	t.Helper()
	path := filepath.Join(t.TempDir(), "algo-review.sqlite3")
	gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	})
	t.Cleanup(func() { gdb.SetConfig(gdb.Config{}) })

	ctx := context.Background()
	for _, ddl := range []string{algoReviewTestDDLPrice, algoReviewTestDDLHome} {
		if _, err := g.DB().Exec(ctx, ddl); err != nil {
			t.Fatalf("建表失败: %v", err)
		}
	}
	return ctx
}

func seedAlgoReviewDay(t *testing.T, ctx context.Context, date string, base float64) {
	t.Helper()
	for h := 0; h < 24; h++ {
		for _, algo := range []string{"timesfm", "gbm"} {
			offset := 0.0
			if algo == "gbm" {
				offset = 10
			}
			p := base + float64(h) + offset
			_, err := g.DB().Exec(ctx, `INSERT INTO gd_price_forecast
				(generated_on, market, grain, algo, target_time, point, q10, q50, q90, model, unit)
				VALUES (?, 'ahead', 'hour', ?, ?, ?, ?, ?, ?, ?, '元/兆瓦时')`,
				date, algo, fmt.Sprintf("%s %02d:00", date, h), p, p-10, p, p+10, "test-"+algo)
			if err != nil {
				t.Fatalf("写留痕失败: %v", err)
			}
		}
		actual := base + float64(h) + 5
		if _, err := g.DB().Exec(ctx, `INSERT INTO gd_home_24h
			(price_date, hour, power_mw, ahead_price, realtime_price, source)
			VALUES (?, ?, 0, ?, ?, 'test')`, date, h, actual, actual+1); err != nil {
			t.Fatalf("写实际值失败: %v", err)
		}
	}
}

func TestGetAlgoReviewAgainstSQLite(t *testing.T) {
	ctx := setupAlgoReviewDB(t)
	seedAlgoReviewDay(t, ctx, "2026-11-10", 100)
	seedAlgoReviewDay(t, ctx, "2026-11-11", 200)

	c := &ControllerV1{}
	res, err := c.GetAlgoReview(ctx, &v1.GetAlgoReviewReq{
		From: "2026-11-10", To: "2026-11-11", Market: "ahead", Date: "2026-11-10",
	})
	if err != nil {
		t.Fatalf("GetAlgoReview 失败: %v", err)
	}
	if res.Market != "ahead" || res.Unit != "元/兆瓦时" {
		t.Errorf("market/unit = %s/%s", res.Market, res.Unit)
	}
	if len(res.Summary) != 3 {
		t.Fatalf("summary 数量 = %d, want 3", len(res.Summary))
	}
	for _, name := range []string{"timesfm", "gbm", "fusion"} {
		found := false
		for _, s := range res.Summary {
			if s.Algo != name {
				continue
			}
			found = true
			if s.Days != 2 || s.Pairs != 48 {
				t.Errorf("%s days/pairs = %d/%d, want 2/48", name, s.Days, s.Pairs)
			}
			if name == "fusion" {
				if s.Coverage != nil {
					t.Errorf("fusion coverage 应为 nil")
				}
				if s.MAE > 1e-9 {
					t.Errorf("fusion MAE = %v, want 0", s.MAE)
				}
			}
		}
		if !found {
			t.Errorf("summary 缺少 %s", name)
		}
	}
	if len(res.Detail) != 72 { // 3 算法 × 24 点
		t.Errorf("detail 数量 = %d, want 72", len(res.Detail))
	}
	if res.DataAsOf != "2026-11-11" || res.LagDays < 0 {
		t.Errorf("dataAsOf/lagDays = %s/%d", res.DataAsOf, res.LagDays)
	}
	if res.Dropped.NoActual != 0 || res.Dropped.Backfilled != 0 || res.Dropped.IncompleteDay != 0 {
		t.Errorf("dropped = %+v, want 全 0", res.Dropped)
	}
}

func TestGetAlgoReviewRejectsBadParams(t *testing.T) {
	ctx := setupAlgoReviewDB(t)
	c := &ControllerV1{}
	cases := []*v1.GetAlgoReviewReq{
		{From: "2026-11-11", To: "2026-11-10"},
		{From: "2026-11-01", To: "2027-12-31"},
		{From: "2026-11-01", To: "2026-11-02", Market: "spot"},
		{From: "2026-11-01", To: "2026-11-02", Algos: "lstm"},
		{From: "2026-11-01", To: "2026-11-02", Date: "2026-11-05"},
	}
	for _, req := range cases {
		if _, err := c.GetAlgoReview(ctx, req); err == nil {
			t.Errorf("参数 %+v 应当报错", req)
		}
	}
}
