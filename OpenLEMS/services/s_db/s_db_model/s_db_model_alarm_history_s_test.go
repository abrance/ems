package s_db_model

import (
	"context"
	"path/filepath"
	"testing"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// 历史告警按点位名称模糊搜索，此前 controller 传 filters["title"]，
// 模型层拼出 `WHERE title LIKE ?`，而 alarm_history 没有 title 列（只有 point_name），
// 前端搜一次就 500：no such column: title。用真 sqlite + 真 SQL 钉住这条路径。
func TestAlarmHistoryGetPageFiltersByPointName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite3")
	gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	})
	t.Cleanup(func() { gdb.SetConfig(gdb.Config{}) })

	ctx := context.Background()
	if _, err := g.DB().Exec(ctx, `
		CREATE TABLE alarm_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id VARCHAR(255) NOT NULL,
			source_device_id VARCHAR(255) NOT NULL,
			point VARCHAR(255) NOT NULL,
			point_name VARCHAR(255) NOT NULL,
			level VARCHAR(255) NOT NULL,
			detail TEXT,
			trigger_at DATETIME NOT NULL,
			clear_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	for _, r := range []struct{ point, pointName string }{
		{"Voltage", "电压过高"},
		{"Power", "功率越限"},
	} {
		if _, err := g.DB().Exec(ctx,
			`INSERT INTO alarm_history (device_id,source_device_id,point,point_name,level,detail,trigger_at)
			 VALUES ('dev-1','dev-1',?,?,'Warn','-',datetime('now'))`,
			r.point, r.pointName); err != nil {
			t.Fatalf("插行失败: %v", err)
		}
	}

	model := &SAlarmHistoryModel{}
	records, total, err := model.GetPage(ctx, 1, 20, map[string]interface{}{
		FieldAlarmHistoryPointName: "电压",
	})
	if err != nil {
		t.Fatalf("按点位名称搜索报错: %v", err)
	}
	if total != 1 || len(records) != 1 {
		t.Fatalf("应只命中 1 行，实际 total=%d len=%d", total, len(records))
	}
	if records[0].PointName != "电压过高" {
		t.Fatalf("命中行不对: %+v", records[0])
	}
}
