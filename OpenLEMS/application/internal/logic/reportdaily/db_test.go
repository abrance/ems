package reportdaily

import (
	"os"
	"path/filepath"
	"testing"

	"application/internal/logic/spotreport"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
	"s_db"
	"s_db/s_db_model"
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

	// 缺省右端必须锚到两侧最新数据日（官网 11-13），不是测试运行时的今天；
	// 否则数据滞后时默认区间会落在数据之后，整页空。
	defaultRes, err := LoadReconcile(ctx, "", "")
	if err != nil {
		t.Fatalf("LoadReconcile 缺省区间失败: %v", err)
	}
	if defaultRes.To != "2026-11-13" {
		t.Fatalf("缺省右端 = %q，期望 2026-11-13（两侧最新日）", defaultRes.To)
	}
	if defaultRes.From != "2026-10-15" {
		t.Fatalf("缺省左端 = %q，期望 2026-10-15（右端往前 30 天）", defaultRes.From)
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

// TestReportDailyUpsertBatchKeepsClosestReportDate 规范 §2：同一 run_date 被多份日报覆盖时，
// 只留「报告日最接近运行日」的那份。活链路（UpsertBatch）原来只判 manual，没有这层判定，
// 后写的远日报会把先写的近日报覆盖掉（依赖写入顺序 = 结果不稳定）。
//
// 双向都验：先近后远、先远后近，两次都必须留 report_date=距离 0 的那行。
func TestReportDailyUpsertBatchKeepsClosestReportDate(t *testing.T) {
	const (
		runDate    = "2026-09-19"
		nearDate   = "2026-09-19" // 距离 0：表1(二) 当日
		farDate    = "2026-09-21" // 距离 2：表2(一) T-2
		needleSQL  = `SELECT report_date || '|' || report_title || '|' || value FROM gd_report_daily WHERE run_date = '2026-09-19' AND dataset = 'day_ahead_trade' AND scope = '发电侧' AND metric = '平均电价'`
		nearExpect = nearDate + "|R(09.19) 表1(二)|431.0"
	)
	row := func(reportDate, title string, value float64) []*s_db_model.SReportDailyModel {
		return []*s_db_model.SReportDailyModel{{
			RunDate: runDate, ReportDate: reportDate, Dataset: "day_ahead_trade",
			Scope: "发电侧", Metric: "平均电价", Value: value, Unit: "元/兆瓦时",
			Source: "表1(二)", Origin: s_db_model.ReportDailyOriginOCR, ReportTitle: title,
		}}
	}

	cases := []struct {
		name    string
		first   []*s_db_model.SReportDailyModel
		second  []*s_db_model.SReportDailyModel
		skipped int
	}{
		{"先近后远", row(nearDate, "R(09.19) 表1(二)", 431.0), row(farDate, "R(09.21) 表2(一)", 999.9), 1},
		{"先远后近", row(farDate, "R(09.21) 表2(一)", 999.9), row(nearDate, "R(09.19) 表1(二)", 431.0), 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			newTestDB(t)
			ctx := t.Context()
			svc := s_db.GetReportDailyService()

			if _, err := svc.UpsertBatch(ctx, c.first, false); err != nil {
				t.Fatalf("首次写入失败: %v", err)
			}
			res, err := svc.UpsertBatch(ctx, c.second, false)
			if err != nil {
				t.Fatalf("二次写入失败: %v", err)
			}
			if res.SkippedFarther != c.skipped {
				t.Fatalf("SkippedFarther = %d，期望 %d（written=%d preserved=%d）",
					res.SkippedFarther, c.skipped, res.Written, res.Preserved)
			}
			if got := scalarStr(t, needleSQL); got != nearExpect {
				t.Fatalf("留下的不是最近报告日那行：%q，期望 %q", got, nearExpect)
			}
			if n := scalar(t, `SELECT COUNT(*) FROM gd_report_daily`); n != 1 {
				t.Fatalf("同一唯一键只应留 1 行，实际 %d", n)
			}
			t.Logf("%s：留下 %q，SkippedFarther=%d", c.name, scalarStr(t, needleSQL), res.SkippedFarther)
		})
	}

	// 等距时保留已有行（先到者胜，与 BackfillFromSpotMetric 的 INSERT OR IGNORE 同口径）
	t.Run("等距保留已有行", func(t *testing.T) {
		newTestDB(t)
		ctx := t.Context()
		svc := s_db.GetReportDailyService()
		if _, err := svc.UpsertBatch(ctx, row("2026-09-18", "R(09.18) 表2(二)", 111.1), false); err != nil {
			t.Fatal(err)
		}
		// 运行日 09-19，距离 1；再写一个报告日 09-20 也是距离 1
		res, err := svc.UpsertBatch(ctx, row("2026-09-20", "R(09.20) 表2(一)", 222.2), false)
		if err != nil {
			t.Fatal(err)
		}
		if res.SkippedFarther != 1 {
			t.Fatalf("等距应保留已有行（SkippedFarther=1），实际 %d", res.SkippedFarther)
		}
		kept := scalarStr(t, needleSQL)
		if kept != "2026-09-18|R(09.18) 表2(二)|111.1" {
			t.Fatalf("等距时不该覆盖已有行，实际 %q", kept)
		}
		t.Logf("等距：保留首次写入的 %q", kept)
	})

	// force=true 行为不变：人工强制重跑就该覆盖
	t.Run("force覆盖远日报", func(t *testing.T) {
		newTestDB(t)
		ctx := t.Context()
		svc := s_db.GetReportDailyService()
		if _, err := svc.UpsertBatch(ctx, row(nearDate, "R(09.19) 表1(二)", 431.0), false); err != nil {
			t.Fatal(err)
		}
		res, err := svc.UpsertBatch(ctx, row(farDate, "R(09.21) 表2(一)", 999.9), true)
		if err != nil {
			t.Fatal(err)
		}
		if res.Written != 1 || res.SkippedFarther != 0 {
			t.Fatalf("force=true 应直接覆盖：written=%d skippedFarther=%d", res.Written, res.SkippedFarther)
		}
		if got := scalarStr(t, needleSQL); got != farDate+"|R(09.21) 表2(一)|999.9" {
			t.Fatalf("force=true 应留下远日报，实际 %q", got)
		}
		t.Logf("force=true：覆盖成 %q", scalarStr(t, needleSQL))
	})
}

// TestReportDailyUpsertBatchWritesNormalRow 正常路径：一行完整字段经活链路落库后原样读回。
//
// 现有用例只断言 report_date/report_title/value（「最接近」判定用到的三列），
// 若 INSERT 的列顺序或字段映射写错（例如 source 与 origin 对调、extractor_version 漏写），
// 那几条仍会绿，但核对页的单位/来源/抽取版本会静默错。这条按列断言落库契约。
func TestReportDailyUpsertBatchWritesNormalRow(t *testing.T) {
	newTestDB(t)
	ctx := t.Context()
	svc := s_db.GetReportDailyService()
	collectedAt := gtime.Now()

	res, err := svc.UpsertBatch(ctx, []*s_db_model.SReportDailyModel{{
		RunDate: "2026-09-19", ReportDate: "2026-09-19",
		Dataset: spotreport.DatasetTrade, Scope: spotreport.ScopeSellSide,
		Metric: spotreport.MetricAvgPrice, Value: 431.0,
		Unit: spotreport.UnitPrice, Source: "表1(二)", Origin: s_db_model.ReportDailyOriginOCR,
		ExtractorVersion: "v1", ReportTitle: "R(09.19) 表1(二)", CollectedAt: collectedAt,
	}}, false)
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if res.Written != 1 || res.Preserved != 0 || res.SkippedFarther != 0 {
		t.Fatalf("正常一行应 written=1 且无保留/跳过，实际 %+v", res)
	}

	rows, err := svc.ListByRunDateRange(ctx, "2026-09-19", "2026-09-19")
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("应读回 1 行，实际 %d", len(rows))
	}
	got := rows[0]
	for _, c := range []struct {
		column, got, want string
	}{
		{"run_date", got.RunDate, "2026-09-19"},
		{"report_date", got.ReportDate, "2026-09-19"},
		{"dataset", got.Dataset, spotreport.DatasetTrade},
		{"scope", got.Scope, spotreport.ScopeSellSide},
		{"metric", got.Metric, spotreport.MetricAvgPrice},
		{"unit", got.Unit, spotreport.UnitPrice},
		{"source", got.Source, "表1(二)"},
		{"origin", got.Origin, s_db_model.ReportDailyOriginOCR},
		{"extractor_version", got.ExtractorVersion, "v1"},
		{"report_title", got.ReportTitle, "R(09.19) 表1(二)"},
	} {
		if c.got != c.want {
			t.Errorf("%s 落库/读回不一致：%q，期望 %q", c.column, c.got, c.want)
		}
	}
	if !near(got.Value, 431.0) {
		t.Errorf("value = %v，期望 431.0", got.Value)
	}
	if got.CollectedAt == nil {
		t.Error("collected_at 丢了：读取回来是 NULL")
	}
	t.Logf("正常一行读回：%s/%s/%s value=%v unit=%q source=%q origin=%q version=%q title=%q",
		got.RunDate, got.Dataset, got.Metric, got.Value, got.Unit, got.Source,
		got.Origin, got.ExtractorVersion, got.ReportTitle)
}

// TestReportDailyUpsertBatchSkipsMissingOrInvalidRows 缺失/异常路径：
// 关键键缺失的行（nil、run_date/dataset/scope/metric 有空）必须整行跳过 ——
// 既不能写进一行空键的残值（核对页会多出一行无 scope 的幽灵数据），
// 也不能因为一条坏数据让整批失败。
func TestReportDailyUpsertBatchSkipsMissingOrInvalidRows(t *testing.T) {
	newTestDB(t)
	ctx := t.Context()
	svc := s_db.GetReportDailyService()

	rows := []*s_db_model.SReportDailyModel{
		nil,
		// 缺 run_date
		{Dataset: spotreport.DatasetTrade, Scope: spotreport.ScopeSellSide,
			Metric: spotreport.MetricAvgPrice, Value: 1, Unit: spotreport.UnitPrice, Origin: OriginOCR},
		// 缺 dataset
		{RunDate: "2026-09-19", ReportDate: "2026-09-19", Scope: spotreport.ScopeSellSide,
			Metric: spotreport.MetricAvgPrice, Value: 2, Unit: spotreport.UnitPrice, Origin: OriginOCR},
		// 缺 scope
		{RunDate: "2026-09-19", ReportDate: "2026-09-19", Dataset: spotreport.DatasetTrade,
			Metric: spotreport.MetricAvgPrice, Value: 3, Unit: spotreport.UnitPrice, Origin: OriginOCR},
		// 缺 metric
		{RunDate: "2026-09-19", ReportDate: "2026-09-19", Dataset: spotreport.DatasetTrade,
			Scope: spotreport.ScopeSellSide, Value: 4, Unit: spotreport.UnitPrice, Origin: OriginOCR},
	}
	res, err := svc.UpsertBatch(ctx, rows, false)
	if err != nil {
		t.Fatalf("缺关键字段的行应被跳过，不应整批报错: %v", err)
	}
	if res.Written != 0 || res.Preserved != 0 || res.SkippedFarther != 0 {
		t.Fatalf("全部是坏行，不应写任何东西，实际 %+v", res)
	}
	if n := scalar(t, `SELECT COUNT(*) FROM gd_report_daily`); n != 0 {
		t.Fatalf("坏行不得落库，表里实际有 %d 行", n)
	}
	list, err := svc.ListByRunDateRange(ctx, "2026-09-19", "2026-09-19")
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("区间内不应有行，实际 %d 行", len(list))
	}
	t.Logf("缺失/异常路径：%d 条坏行全部跳过，written=0，表内 0 行", len(rows))
}
