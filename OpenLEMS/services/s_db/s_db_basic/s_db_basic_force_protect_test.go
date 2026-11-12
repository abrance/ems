package s_db_basic_test

// 为什么这条用例放在 s_db_basic 的「外部测试包」里，而不是 impl 包的测试里：
// g.DB() 返回的实例是进程级单例（第一次取用后不再跟着 gdb.SetConfig 变），
// impl 包的测试二进制里 alarm_history 那批用例已经先用 default 组建过库，
// 再在这里 SetConfig 会拿到别的用例的库（表现为 PRAGMA 报 unable to open database file）。
// 契约本身就写在 s_db_basic.ISpotExtractService.ReplaceMetrics 上，外部测试包导入 impl 也不产生环。

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"

	"s_db/internal/impl"
	"s_db/s_db_model"
)

// spotMetricTestDDL 与 internal.Init() 里 gd_spot_metric / gd_spot_metric_audit 的建表语句一致
// （唯一键相同，才能真正复现「先删后插撞唯一键 / 权威行被整段删掉」）。
const spotMetricTestDDL = `
	CREATE TABLE gd_spot_metric (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		report_date VARCHAR(10) NOT NULL,
		run_date VARCHAR(10) NOT NULL,
		dataset VARCHAR(32) NOT NULL,
		scope VARCHAR(32) NOT NULL DEFAULT '',
		metric VARCHAR(32) NOT NULL,
		value REAL NOT NULL DEFAULT 0,
		unit VARCHAR(16) NOT NULL DEFAULT '',
		at_time VARCHAR(8) NOT NULL DEFAULT '',
		text_value VARCHAR(32) NOT NULL DEFAULT '',
		mom_pct REAL,
		share_pct REAL,
		source VARCHAR(32) NOT NULL DEFAULT '',
		conf REAL NOT NULL DEFAULT 0,
		extractor_version VARCHAR(16) NOT NULL DEFAULT '',
		origin VARCHAR(8) NOT NULL DEFAULT 'ocr',
		edited_by VARCHAR(64) NOT NULL DEFAULT '',
		edited_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (report_date, run_date, dataset, scope, metric)
	);
	CREATE TABLE gd_spot_metric_audit (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		report_date VARCHAR(10) NOT NULL,
		run_date VARCHAR(10) NOT NULL,
		dataset VARCHAR(32) NOT NULL,
		scope VARCHAR(32) NOT NULL DEFAULT '',
		metric VARCHAR(32) NOT NULL,
		old_value REAL,
		new_value REAL,
		old_origin VARCHAR(8) NOT NULL DEFAULT '',
		operation VARCHAR(32) NOT NULL DEFAULT '',
		operator VARCHAR(64) NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
`

// newSpotMetricTestDB 建临时 sqlite 库并建出上面两张表。
func newSpotMetricTestDB(t *testing.T) context.Context {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db.sqlite3")
	if err := gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	}); err != nil {
		t.Fatalf("设置数据库配置失败: %v", err)
	}
	t.Cleanup(func() { _ = gdb.SetConfig(gdb.Config{}) })

	ctx := context.Background()
	if _, err := g.DB().Exec(ctx, spotMetricTestDDL); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	return ctx
}

func resetSpotMetricTestRows(t *testing.T, ctx context.Context) {
	t.Helper()
	for _, table := range []string{s_db_model.TableSpotMetric, s_db_model.TableSpotMetricAudit} {
		if _, err := g.DB().Exec(ctx, "DELETE FROM "+table); err != nil {
			t.Fatalf("清空 %s 失败: %v", table, err)
		}
	}
}

// force 只允许覆盖未复核的 ocr/cv 值。2026-11-12 用 force=true 补采时，10-16 / 10-25
// 各 8 行 origin='pm-api' 被整段删掉：旧实现 force 分支不设 keptIDs，先删后插只写回
// 本次 OCR 提取到的键，没被重新提取到的官方直采值就永久消失（只剩 16 条 overwritten_by_extract 审计）。
//
// 这张 4 种 origin × force true/false 的表钉住新口径：pm-api / manual 在任何 force 下
// 值都原样保留，且不再写 overwritten_by_extract 审计。
func TestReplaceMetricsForceNeverOverwritesAuthoritativeOrigins(t *testing.T) {
	ctx := newSpotMetricTestDB(t)
	const (
		reportDate = "2026-10-16"
		runDate    = "2026-10-16"
		dataset    = "node_price_peak"
		scope      = "韶关"
		metricName = "价格档位"
	)

	cases := []struct {
		origin   string
		force    bool
		wantKept bool // true = 原值 100 与原 origin 必须原样保留
	}{
		{origin: "ocr", force: false},
		{origin: "ocr", force: true},
		{origin: "cv", force: false},
		{origin: "cv", force: true},
		{origin: "pm-api", force: false, wantKept: true},
		{origin: "pm-api", force: true, wantKept: true},
		{origin: "manual", force: false, wantKept: true},
		{origin: "manual", force: true, wantKept: true},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("origin=%s/force=%v", c.origin, c.force), func(t *testing.T) {
			resetSpotMetricTestRows(t, ctx)
			if _, err := g.DB().Model(s_db_model.TableSpotMetric).Ctx(ctx).Data(g.Map{
				s_db_model.FieldSpotMetricReportDate: reportDate,
				s_db_model.FieldSpotMetricRunDate:    runDate,
				s_db_model.FieldSpotMetricDataset:    dataset,
				s_db_model.FieldSpotMetricScope:      scope,
				s_db_model.FieldSpotMetricMetric:     metricName,
				s_db_model.FieldSpotMetricValue:      100,
				s_db_model.FieldSpotMetricOrigin:     c.origin,
			}).Insert(); err != nil {
				t.Fatalf("写入已有指标失败: %v", err)
			}

			incoming := []*s_db_model.SSpotMetricModel{{
				ReportDate: reportDate, RunDate: runDate, Dataset: dataset,
				Scope: scope, Metric: metricName, Value: 200, Origin: "ocr",
			}}
			res, err := impl.GetSpotExtractService().ReplaceMetrics(ctx, reportDate, incoming, c.force)
			if err != nil {
				t.Fatalf("ReplaceMetrics 失败: %v", err)
			}

			rows, err := g.DB().Model(s_db_model.TableSpotMetric).Ctx(ctx).All()
			if err != nil {
				t.Fatalf("读回指标失败: %v", err)
			}
			if len(rows) != 1 {
				t.Fatalf("该日报应恰好 1 行，实际 %d 行: %v", len(rows), rows)
			}
			value := rows[0][s_db_model.FieldSpotMetricValue].Float64()
			origin := rows[0][s_db_model.FieldSpotMetricOrigin].String()

			if c.wantKept {
				if value != 100 {
					t.Fatalf("force=%v 下 origin=%s 的值被改掉了：%v，期望 100", c.force, c.origin, value)
				}
				if origin != c.origin {
					t.Fatalf("origin 被改写：%s，期望 %s", origin, c.origin)
				}
				if c.force {
					if len(res.Skipped) != 1 || res.Skipped[0].Origin != c.origin || res.Skipped[0].OldValue != 100 {
						t.Fatalf("force=true 应产出 1 条跳过告警（origin=%s，旧值 100），实际 %+v", c.origin, res.Skipped)
					}
				} else if len(res.Skipped) != 0 || res.Preserved != 1 {
					t.Fatalf("force=false 应静默保留（Preserved=1），实际 Preserved=%d Skipped=%+v", res.Preserved, res.Skipped)
				}
				if len(res.Overwritten) != 0 {
					t.Fatalf("force=%v 不该再覆盖权威行，实际 Overwritten=%+v", c.force, res.Overwritten)
				}
			} else {
				if value != 200 || origin != "ocr" {
					t.Fatalf("origin=%s force=%v 应被本次解析替换为 200/ocr，实际 %v/%s", c.origin, c.force, value, origin)
				}
				if len(res.Skipped) != 0 {
					t.Fatalf("非权威行不该进 Skipped：%+v", res.Skipped)
				}
			}

			audits, err := g.DB().Model(s_db_model.TableSpotMetricAudit).Ctx(ctx).
				Where(s_db_model.FieldSpotMetricAuditOp, s_db_model.SpotMetricAuditOverwrite).Count()
			if err != nil {
				t.Fatalf("查询审计表失败: %v", err)
			}
			if audits != 0 {
				t.Fatalf("不应再写 overwritten_by_extract 审计，实际 %d 条", audits)
			}
		})
	}
}
