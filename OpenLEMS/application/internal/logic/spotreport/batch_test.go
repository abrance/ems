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

// 批量重跑默认跳过「全部已确认」的日报（pending=0 且 confirmed>0）：
// 只要还有未复核项（ocr 或 cv）就说明没复核完；完全没有确认状态的日报可以随意重跑。
func TestSkipConfirmedReport(t *testing.T) {
	cases := []struct {
		pending, confirmed int
		want               bool
	}{
		{pending: 0, confirmed: 24, want: true},   // 全部确认 → 跳过
		{pending: 23, confirmed: 24, want: false}, // 还有地图预填未确认 → 重跑
		{pending: 58, confirmed: 24, want: false}, // 回归：58 项 ocr 未确认 + 24 项已确认，不能跳过
		{pending: 0, confirmed: 0, want: false},   // 未解析 → 重跑
		{pending: 82, confirmed: 0, want: false},  // 刚解析、全未确认 → 重跑
	}
	for _, c := range cases {
		if got := skipConfirmedReport(c.pending, c.confirmed); got != c.want {
			t.Fatalf("pending=%d confirmed=%d 期望 %v，实际 %v", c.pending, c.confirmed, c.want, got)
		}
	}
}
