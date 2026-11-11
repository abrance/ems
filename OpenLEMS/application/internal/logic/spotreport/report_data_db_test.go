package spotreport

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/gogf/gf/v2/database/gdb"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"

	"s_db/s_db_model"
)

// 端到端：真实 sqlite + 真实 SQL 过滤。
//
// 守两件纯函数测不出来的事：
//  1. 作废行（origin=void）不得出现在 GetReportData 返回里 —— 之前 ListMetrics 没有 origin 过滤，
//     09-25 这类「OCR 旧值被 pm-api 取代」的日报会把 80 条作废值照样吐给前端，页面挂个
//     「未复核」标签当成真数据显示。
//  2. 老库里 origin 为 NULL 的行不能被误删（SQL 里 NULL <> 'void' 是 NULL）。
func TestGetReportDataExcludesVoidAgainstSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("打开临时库失败: %v", err)
	}
	for _, ddl := range []string{
		`CREATE TABLE gd_spot_metric (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			report_date VARCHAR(10) NOT NULL, run_date VARCHAR(10) NOT NULL,
			dataset VARCHAR(32) NOT NULL, scope VARCHAR(32) NOT NULL DEFAULT '',
			metric VARCHAR(32) NOT NULL, value REAL NOT NULL DEFAULT 0,
			unit VARCHAR(16) NOT NULL DEFAULT '', at_time VARCHAR(8) NOT NULL DEFAULT '',
			text_value VARCHAR(32) NOT NULL DEFAULT '', mom_pct REAL, share_pct REAL,
			source VARCHAR(32) NOT NULL DEFAULT '', conf REAL NOT NULL DEFAULT 0,
			extractor_version VARCHAR(16) NOT NULL DEFAULT '',
			origin VARCHAR(8), edited_by VARCHAR(64) NOT NULL DEFAULT '',
			edited_at DATETIME, created_at DATETIME, updated_at DATETIME,
			UNIQUE (report_date, run_date, dataset, scope, metric))`,
		`CREATE TABLE gd_spot_extract_run (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			report_date VARCHAR(10) NOT NULL, status VARCHAR(16) NOT NULL DEFAULT '',
			ocr_model VARCHAR(32) NOT NULL DEFAULT '', page_count INTEGER NOT NULL DEFAULT 0,
			ocr_ms INTEGER NOT NULL DEFAULT 0, warnings TEXT, payload TEXT,
			extractor_version VARCHAR(16) NOT NULL DEFAULT '',
			created_at DATETIME, updated_at DATETIME)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("建表失败: %v", err)
		}
	}

	// 09-25 形状：同一份日报里 void 旧值（含档位两类）+ pm-api 日频 + 一行老库空 origin。
	type row struct {
		dataset, scope, metric, unit, origin, textValue string
		value                                           float64
	}
	for _, r := range []row{
		{"summary", "正文", "日前加权平均电价", "元/兆瓦时", OriginPmAPI, "", 362.69},
		{"day_ahead_trade", "发电侧", "日前总成交电量", "MWh", OriginPmAPI, "", 1500776},
		{"offtake_trade", "用电侧", "日前总成交电量", "MWh", OriginPmAPI, "", 2384679},
		{"node_price_peak", "惠州", "价格档位", "元/千瓦时", OriginVoid, "[800,+inf)", 4},
		{"node_price_peak", "揭阳", "价格档位", "元/千瓦时", OriginVoid, "[800,+inf)", 4},
		{"node_price_valley", "梅州", "价格档位", "元/千瓦时", OriginVoid, "[0,100)", 0},
		{"realtime_trade", "发电侧", "实时加权平均电价", "元/兆瓦时", "", "", 266.69},
	} {
		if _, err := db.Exec(
			`INSERT INTO gd_spot_metric (report_date,run_date,dataset,scope,metric,value,unit,text_value,origin)
			 VALUES ('2026-09-25','2026-09-25',?,?,?,?,?,?,?)`,
			r.dataset, r.scope, r.metric, r.value, r.unit, r.textValue, r.origin); err != nil {
			t.Fatalf("插行失败: %v", err)
		}
	}
	db.Close()

	gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	})
	t.Cleanup(func() { gdb.SetConfig(gdb.Config{}) })

	res, err := GetReportData(context.Background(), "2026-09-25")
	if err != nil {
		t.Fatalf("GetReportData 失败: %v", err)
	}

	// 3 行 pm-api + 1 行空 origin；3 行 void 一条都不该出现
	if len(res.Metrics) != 4 {
		t.Fatalf("期望 4 行（3 pm-api + 1 空 origin），实际 %d 行", len(res.Metrics))
	}
	peak, valley := 0, 0
	models := make([]*s_db_model.SSpotMetricModel, 0, len(res.Metrics))
	for _, m := range res.Metrics {
		if m.Origin == OriginVoid {
			t.Fatalf("作废行漏到页面数据里: %s/%s/%s", m.Dataset, m.Scope, m.Metric)
		}
		if m.Dataset == DatasetNodePricePeak {
			peak++
		}
		if m.Dataset == DatasetNodePriceValley {
			valley++
		}
		models = append(models, &s_db_model.SSpotMetricModel{Origin: m.Origin})
	}
	if peak != 0 || valley != 0 {
		t.Fatalf("档位两类应为真空态，实际 peak=%d valley=%d", peak, valley)
	}

	pending, confirmed := CountReview(models)
	if pending != 1 || confirmed != 3 {
		t.Fatalf("CountReview = (%d, %d)，期望 (1, 3)：void 不进任何队列、空 origin 仍算未复核", pending, confirmed)
	}
}
