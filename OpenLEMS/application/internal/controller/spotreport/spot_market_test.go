package spotreport

import (
	"context"
	"path/filepath"
	"testing"

	v1 "application/api/spotreport/v1"
	"application/internal/logic/spotmarket"
	"s_db/s_db_model"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// 端到端（控制器 → 逻辑 → 真 sqlite）：守前端读的 JSON 字段名。
// 只看 Go 字段会漏掉 json tag 写错 — 那种错误编译全绿、页面拿到空串。
func TestGetSpotMarketLatestEndpointShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite3")
	gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	})
	t.Cleanup(func() { gdb.SetConfig(gdb.Config{}) })

	ctx := context.Background()
	if _, err := g.DB().Exec(ctx, `
		CREATE TABLE gd_spot_metric (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			report_date VARCHAR(10) NOT NULL, run_date VARCHAR(10) NOT NULL,
			dataset VARCHAR(32) NOT NULL, scope VARCHAR(32) NOT NULL DEFAULT '',
			metric VARCHAR(32) NOT NULL, value REAL NOT NULL DEFAULT 0,
			unit VARCHAR(16) NOT NULL DEFAULT '', at_time VARCHAR(8) NOT NULL DEFAULT '',
			text_value VARCHAR(32) NOT NULL DEFAULT '', mom_pct REAL, share_pct REAL,
			source VARCHAR(32) NOT NULL DEFAULT '', conf REAL NOT NULL DEFAULT 0,
			extractor_version VARCHAR(16) NOT NULL DEFAULT '',
			origin VARCHAR(8) NOT NULL DEFAULT 'ocr', edited_by VARCHAR(64) NOT NULL DEFAULT '',
			edited_at DATETIME, created_at DATETIME, updated_at DATETIME,
			UNIQUE (report_date, run_date, dataset, scope, metric))`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	for _, r := range [][]any{
		{"2026-11-11", "2026-11-11", spotmarket.DatasetAhead, "发电侧", "日前加权平均电价", 360.0, "元/兆瓦时", s_db_model.SpotOriginPmAPI},
		{"2026-11-15", "2026-11-15", spotmarket.DatasetAhead, "发电侧", "作废新值", 999.0, "元/兆瓦时", s_db_model.SpotOriginVoid},
	} {
		if _, err := g.DB().Exec(ctx, `INSERT INTO gd_spot_metric
			(report_date, run_date, dataset, scope, metric, value, unit, origin)
			VALUES (?,?,?,?,?,?,?,?)`, r...); err != nil {
			t.Fatalf("插行失败: %v", err)
		}
	}

	c := &ControllerV1{}
	res, err := c.GetSpotMarketLatest(ctx, &v1.GetSpotMarketLatestReq{})
	if err != nil {
		t.Fatalf("GetSpotMarketLatest 失败: %v", err)
	}
	got := jsonShape(t, res)
	if len(got) != 1 || got["date"] != "2026-11-11" {
		t.Fatalf("响应应为 {\"date\":\"2026-11-11\"}，实际 %v", got)
	}
}
