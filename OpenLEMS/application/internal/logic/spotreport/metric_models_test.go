package spotreport

import "testing"

// 映射回归：origin / text_value 这类语义字段漏映射不会报错，但会让「预填」被当成
// 普通 OCR 值入库（线上曾漏过 origin）。这里逐字段锚定。
func TestMetricModelsKeepsSemanticFields(t *testing.T) {
	mom, share := 8.39, 37.64
	models := metricModels("2026-09-21", []Metric{
		{
			RunDate: "2026-09-21", Dataset: DatasetNodePricePeak, Scope: "韶关",
			Metric: MetricPriceBucket, Value: 5, TextValue: "[800,+inf)",
			Source: "图2-1", Conf: 0.95, Origin: OriginCV,
		},
		{
			RunDate: "2026-09-21", Dataset: DatasetTrade, Scope: ScopeSellSide,
			Metric: MetricAvgPrice, Value: 573.57, Unit: UnitPrice,
			MomPct: &mom, SharePct: &share, Source: SourceTrade, Conf: 1,
		},
	})
	if len(models) != 2 {
		t.Fatalf("期望 2 条，实际 %d", len(models))
	}
	first := models[0]
	if first.Origin != OriginCV {
		t.Fatalf("origin 应为 %s，实际 %q", OriginCV, first.Origin)
	}
	if first.TextValue != "[800,+inf)" {
		t.Fatalf("text_value 丢失：%q", first.TextValue)
	}
	if first.ReportDate != "2026-09-21" || first.RunDate != "2026-09-21" {
		t.Fatalf("日报日/运行日映射错误：%+v", first)
	}
	second := models[1]
	if second.Origin != "" {
		t.Fatalf("未标注 origin 的指标应保持空（由存储层默认 ocr），实际 %q", second.Origin)
	}
	if second.MomPct == nil || *second.MomPct != mom || second.SharePct == nil || *second.SharePct != share {
		t.Fatalf("环比/占比丢失：%+v", second)
	}
	if second.Unit != UnitPrice {
		t.Fatalf("单位丢失：%q", second.Unit)
	}
	if second.ExtractorVersion != ExtractorVersion {
		t.Fatalf("提取器版本未写入：%q", second.ExtractorVersion)
	}
}
