package reportdaily

import (
	"os"
	"path/filepath"
	"testing"

	"application/internal/logic/spotreport"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// 真库用例：本包的写路径（persistExtracted）与核对读路径（LoadReconcile）各跑一遍真 sqlite。
//
// 纯函数单测守不住 A2 那条口径（「提取前后旧表 COUNT 与 MAX(updated_at) 完全相同」）——
// 那是「有没有碰到那张表」的问题，只有真库才看得见。
func newTestDB(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db.sqlite3")
	if err := gdb.SetConfig(gdb.Config{"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}}}); err != nil {
		t.Fatalf("设置数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = gdb.SetConfig(gdb.Config{}) })
	for _, ddl := range []string{
		`CREATE TABLE gd_report_daily (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_date VARCHAR(10) NOT NULL, report_date VARCHAR(10) NOT NULL,
			dataset VARCHAR(32) NOT NULL, scope VARCHAR(16) NOT NULL, metric VARCHAR(16) NOT NULL,
			value REAL NOT NULL, unit VARCHAR(16) NOT NULL, source VARCHAR(16) NOT NULL,
			origin VARCHAR(8) NOT NULL DEFAULT 'ocr', extractor_version VARCHAR(16) NOT NULL DEFAULT '',
			report_title VARCHAR(255) NOT NULL DEFAULT '', collected_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (run_date, dataset, scope, metric))`,
		`CREATE INDEX idx_report_daily_run_date ON gd_report_daily (run_date)`,
		`CREATE TABLE gd_spot_metric (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			report_date VARCHAR(10) NOT NULL, run_date VARCHAR(10) NOT NULL,
			dataset VARCHAR(32) NOT NULL, scope VARCHAR(32) NOT NULL DEFAULT '',
			metric VARCHAR(32) NOT NULL, value REAL NOT NULL DEFAULT 0,
			unit VARCHAR(16) NOT NULL DEFAULT '', at_time VARCHAR(8) NOT NULL DEFAULT '',
			text_value VARCHAR(32) NOT NULL DEFAULT '', mom_pct REAL, share_pct REAL,
			source VARCHAR(32) NOT NULL DEFAULT '', conf REAL NOT NULL DEFAULT 0,
			extractor_version VARCHAR(16) NOT NULL DEFAULT '', origin VARCHAR(8) NOT NULL DEFAULT 'ocr',
			edited_by VARCHAR(64) NOT NULL DEFAULT '', edited_at DATETIME,
			created_at DATETIME, updated_at DATETIME,
			UNIQUE (report_date, run_date, dataset, scope, metric))`,
	} {
		if _, err := g.DB().Exec(t.Context(), ddl); err != nil {
			t.Fatalf("建表失败: %v", err)
		}
	}
}

// TestReportDailyWritePathNeverTouchesOldTable A2：提取前后旧表 COUNT 与 MAX(updated_at) 完全相同。
func TestReportDailyWritePathNeverTouchesOldTable(t *testing.T) {
	newTestDB(t)
	ctx := t.Context()
	if _, err := g.DB().Exec(ctx, `
		INSERT INTO gd_spot_metric (report_date,run_date,dataset,scope,metric,value,unit,origin,created_at,updated_at)
		VALUES ('2026-09-19','2026-09-19','day_ahead_trade','发电侧','发电侧日前加权平均电价',428.95,'元/兆瓦时','pm-api','2026-09-20 01:00:00','2026-09-20 01:00:00')`); err != nil {
		t.Fatal(err)
	}
	before := scalar(t, `SELECT COUNT(*) FROM gd_spot_metric`)
	beforeMax := scalarStr(t, `SELECT COALESCE(MAX(updated_at),'') FROM gd_spot_metric`)

	metrics := []spotreport.Metric{
		{RunDate: "2026-09-19", Dataset: spotreport.DatasetTrade, Scope: spotreport.ScopeSellSide,
			Metric: spotreport.MetricAvgPrice, Value: 428.95, Unit: spotreport.UnitPrice, Source: spotreport.SourceTrade},
		{RunDate: "2026-09-19", Dataset: spotreport.DatasetDecl, Scope: spotreport.ScopeAbove600,
			Metric: spotreport.MetricDeclVolume, Value: 98000, Unit: spotreport.UnitMW, Source: spotreport.SourceDecl},
	}
	written, subsetCount, err := persistExtracted(ctx, "2026-09-19", "通报 | 广东电力现货市场2026年9月运行日报(09.19)", metrics, false)
	if err != nil {
		t.Fatalf("写日报原值失败: %v", err)
	}
	if written.Written != 2 || subsetCount != 2 {
		t.Fatalf("应写 2 行，实际 written=%d subset=%d", written.Written, subsetCount)
	}

	after := scalar(t, `SELECT COUNT(*) FROM gd_spot_metric`)
	afterMax := scalarStr(t, `SELECT COALESCE(MAX(updated_at),'') FROM gd_spot_metric`)
	if before != after || beforeMax != afterMax {
		t.Fatalf("A2 破了：旧表被写入。COUNT %d→%d，MAX(updated_at) %q→%q", before, after, beforeMax, afterMax)
	}
	t.Logf("A2 提取前后旧表：COUNT=%d，MAX(updated_at)=%q（完全一致）", after, afterMax)

	// 幂等 + manual 保留
	if _, err := g.DB().Exec(ctx, `UPDATE gd_report_daily SET origin='manual', value=999 WHERE metric='平均电价'`); err != nil {
		t.Fatal(err)
	}
	res, _, err := persistExtracted(ctx, "2026-09-19", "重跑", metrics, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Preserved != 1 {
		t.Fatalf("force=false 时应保留 1 行 manual，实际 %d", res.Preserved)
	}
	if v := scalarFloat(t, `SELECT value FROM gd_report_daily WHERE metric='平均电价'`); v != 999 {
		t.Fatalf("manual 行被覆盖了：%v", v)
	}
	t.Logf("重跑（force=false）：保留 manual %d 行，人工值 999 未被覆盖", res.Preserved)
}

// TestReportDailyLoadReconcileRealDB 核对读路径跑真库：11-12/11-13 无日报 → reportValue=null。
func TestReportDailyLoadReconcileRealDB(t *testing.T) {
	newTestDB(t)
	ctx := t.Context()
	// 左列：11-10、11-11 有日报；11-12/11-13 没有
	if _, err := g.DB().Exec(ctx, `
		INSERT INTO gd_report_daily (run_date,report_date,dataset,scope,metric,value,unit,source,origin)
		VALUES ('2026-11-11','2026-11-11','day_ahead_trade','发电侧','平均电价',463.27,'元/兆瓦时','表1(二)','ocr'),
		       ('2026-11-11','2026-11-11','day_ahead_trade','燃煤','平均电价',412.0,'元/兆瓦时','表1(二)','ocr')`); err != nil {
		t.Fatal(err)
	}
	// 右列：11-10 ~ 11-13 都有官方值
	for _, day := range []string{"2026-11-10", "2026-11-11", "2026-11-12", "2026-11-13"} {
		if _, err := g.DB().Exec(ctx, `
			INSERT INTO gd_spot_metric (report_date,run_date,dataset,scope,metric,value,unit,origin)
			VALUES (?,?,'day_ahead_trade','发电侧','发电侧日前加权平均电价',463.25,'元/兆瓦时','pm-api')`,
			day, day); err != nil {
			t.Fatal(err)
		}
	}

	res, err := LoadReconcile(ctx, "2026-11-10", "2026-11-13")
	if err != nil {
		t.Fatalf("LoadReconcile 失败: %v", err)
	}
	if res.AsOfReport != "2026-11-11" {
		t.Fatalf("asOfReport = %q，期望 2026-11-11", res.AsOfReport)
	}
	if res.AsOfOfficial != "2026-11-13" {
		t.Fatalf("asOfOfficial = %q，期望 2026-11-13", res.AsOfOfficial)
	}
	if res.OfficialOrigin != "pm-api" {
		t.Fatalf("officialOrigin = %q", res.OfficialOrigin)
	}
	if len(res.Days) != 4 {
		t.Fatalf("应有 4 天（官网有 4 天），实际 %d", len(res.Days))
	}
	for _, day := range res.Days {
		switch day.RunDate {
		case "2026-11-12", "2026-11-13":
			if !day.MissingReport {
				t.Fatalf("%s 应标记缺日报", day.RunDate)
			}
			for _, it := range day.Items {
				if it.ReportValue != nil {
					t.Fatalf("%s reportValue 必须是 null（不是 0），实际 %v", day.RunDate, *it.ReportValue)
				}
				if it.OfficialValue == nil {
					t.Fatalf("%s 右列应照常给值", day.RunDate)
				}
			}
			t.Logf("%s 缺日报：reportValue=null（非 0），officialValue=%v", day.RunDate, *day.Items[0].OfficialValue)
		case "2026-11-11":
			if day.MissingReport {
				t.Fatalf("11-11 有日报")
			}
			var ahead, coal *bool
			for _, it := range day.Items {
				switch {
				case it.Dataset == "day_ahead_trade" && it.Scope == "发电侧":
					ahead = ptr(it.ReportValue != nil)
				case it.Dataset == "day_ahead_trade" && it.Scope == "燃煤":
					coal = ptr(it.OfficialValue == nil && it.Compare == CompareOfficialMissing)
				}
			}
			if ahead == nil || !*ahead {
				t.Fatal("11-11 发电侧应有日报原值")
			}
			if coal == nil || !*coal {
				t.Fatal("11-11 燃煤应判 official_missing 且官网值 null")
			}
		}
	}
}

func ptr(b bool) *bool { return &b }

func scalar(t *testing.T, sql string) int {
	t.Helper()
	v, err := g.DB().GetValue(t.Context(), sql)
	if err != nil {
		t.Fatal(err)
	}
	return v.Int()
}

func scalarStr(t *testing.T, sql string) string {
	t.Helper()
	v, err := g.DB().GetValue(t.Context(), sql)
	if err != nil {
		t.Fatal(err)
	}
	return v.String()
}

func scalarFloat(t *testing.T, sql string) float64 {
	t.Helper()
	v, err := g.DB().GetValue(t.Context(), sql)
	if err != nil {
		t.Fatal(err)
	}
	return v.Float64()
}

var _ = os.Getenv
