package spotreport

import "testing"

// 覆盖计数依赖告警前缀，改动文案时要同步（避免批量任务把覆盖数静默算成 0）
func TestCountOverwritten(t *testing.T) {
	warnings := []string{
		"重跑覆盖人工修订：运行日 2026-09-21 发电侧/平均电价（表1(二)）人工值 999 → OCR 值 573.57",
		"重跑覆盖人工修订：运行日 2026-09-19 燃气/气机容量（表2(一)）人工值 14876.7 → OCR 值 14876.72",
		"校验告警：正文与表格不一致",
		"地图上有 1 个地市未能判定档位",
	}
	if got := countOverwritten(warnings); got != 2 {
		t.Fatalf("期望数出 2 条覆盖，实际 %d", got)
	}
	if got := countOverwritten(nil); got != 0 {
		t.Fatalf("空告警应为 0，实际 %d", got)
	}
}
