package spotreport

import (
	"strings"
	"testing"

	"s_db/s_db_basic"
)

// 重跑以 OCR 为准，但覆盖人工修订必须产生可读告警（否则人工判断无声消失）
func TestOverwriteWarnings(t *testing.T) {
	newValue := 573.57
	warnings := overwriteWarnings([]s_db_basic.SpotMetricOverwrite{
		{
			Key:      s_db_basic.SpotMetricKey{RunDate: "2026-09-21", Scope: "发电侧", Metric: MetricAvgPrice, SourceLabel: "表1(二)"},
			OldValue: 580,
			NewValue: &newValue,
		},
		{
			Key:      s_db_basic.SpotMetricKey{RunDate: "2026-09-19", Scope: "燃气", Metric: MetricGasCap, SourceLabel: "表2(一)"},
			OldValue: 14876.7,
		},
	})
	if len(warnings) != 2 {
		t.Fatalf("期望 2 条告警，实际 %d：%v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "人工值 580") || !strings.Contains(warnings[0], "OCR 值 573.57") {
		t.Fatalf("告警未包含新旧值：%s", warnings[0])
	}
	if !strings.Contains(warnings[1], "本次未提取到") {
		t.Fatalf("未提取到的字段应显式说明：%s", warnings[1])
	}
	if !strings.Contains(warnings[0], "发电侧") || !strings.Contains(warnings[0], "表1(二)") {
		t.Fatalf("告警应含对象与来源：%s", warnings[0])
	}
}
