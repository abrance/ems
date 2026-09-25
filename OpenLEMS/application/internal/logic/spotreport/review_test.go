package spotreport

import (
	"strings"
	"testing"

	"s_db/s_db_basic"
	"s_db/s_db_model"
)

// 复核状态机口径：ocr/cv/空 = 未复核，只有 manual = 已确认。
// 这条口径同时决定首页「待确认 N 项」徽标、确认接口的选中范围、批量重跑的跳过判定，
// 口径漂了会出现「点了一键确认但徽标还在」这类现象。
func TestReviewState(t *testing.T) {
	cases := []struct {
		origin   string
		expected bool
	}{
		{OriginManual, true},
		{OriginOCR, false},
		{OriginCV, false},
		{"", false}, // 老库没有 origin 列的行
		{"Manual", false},
	}
	for _, c := range cases {
		if got := IsConfirmed(c.origin); got != c.expected {
			t.Fatalf("IsConfirmed(%q) = %v，期望 %v", c.origin, got, c.expected)
		}
	}

	metrics := []*s_db_model.SSpotMetricModel{
		{Origin: OriginManual}, {Origin: OriginManual},
		{Origin: OriginOCR}, {Origin: OriginCV}, {Origin: ""},
	}
	pending, confirmed := CountReview(metrics)
	if pending != 3 || confirmed != 2 {
		t.Fatalf("CountReview = (%d, %d)，期望 (3, 2)", pending, confirmed)
	}

	pending, confirmed = CountReview(nil)
	if pending != 0 || confirmed != 0 {
		t.Fatalf("空列表应返回 (0, 0)，实际 (%d, %d)", pending, confirmed)
	}
}

// 保留告警不能算进批量任务的「覆盖数」：两个前缀必须不同，
// 且中文前缀要用 strings.HasPrefix 判断（曾用字节切片判断导致覆盖数恒为 0）。
func TestPreservedWarningsNotCountedAsOverwritten(t *testing.T) {
	newValue := 4.0
	warnings := preservedWarnings([]s_db_basic.SpotMetricOverwrite{
		{
			Key:      s_db_basic.SpotMetricKey{RunDate: "2026-09-21", Scope: "韶关", Metric: "价格档位", SourceLabel: "图2-1"},
			OldValue: 5,
			NewValue: &newValue,
		},
		// 本次没提取到该字段时不该产生保留告警
		{Key: s_db_basic.SpotMetricKey{RunDate: "2026-09-21", Scope: "惠州", Metric: "价格档位"}, OldValue: 5},
	})
	if len(warnings) != 1 {
		t.Fatalf("期望 1 条保留告警，实际 %d（%v）", len(warnings), warnings)
	}
	if got := countOverwritten(warnings); got != 0 {
		t.Fatalf("保留告警不该被算成覆盖，实际覆盖数 %d", got)
	}
	if !strings.Contains(warnings[0], "已确认 5") || !strings.Contains(warnings[0], "本次解析值 4") {
		t.Fatalf("保留告警文案缺少新旧值：%s", warnings[0])
	}
}
