package impl

import (
	"testing"

	"s_db/s_db_model"
)

// dropConfirmedIncoming 是「再次解析不覆盖已确认值」的核心：
// 同键的新提取值必须被丢掉，否则插入时撞唯一键（report_date+run_date+dataset+scope+metric），
// 或更糟——把已确认值换成 OCR 值却仍标着 manual。
func TestDropConfirmedIncoming(t *testing.T) {
	confirmed := map[string]bool{
		metricKey("2026-09-21", "2026-09-21", "node_price_peak", "韶关", "价格档位"): true,
	}
	incoming := []*s_db_model.SSpotMetricModel{
		{ReportDate: "2026-09-21", RunDate: "2026-09-21", Dataset: "node_price_peak", Scope: "韶关", Metric: "价格档位", Value: 4},
		{ReportDate: "2026-09-21", RunDate: "2026-09-21", Dataset: "node_price_peak", Scope: "惠州", Metric: "价格档位", Value: 5},
		{ReportDate: "2026-09-21", RunDate: "2026-09-21", Dataset: "summary", Scope: "市场", Metric: "日前加权平均电价", Value: 573.57},
	}
	kept := dropConfirmedIncoming(confirmed, incoming)
	if len(kept) != 2 {
		t.Fatalf("应丢弃 1 条已确认同键值，实际保留 %d 条", len(kept))
	}
	for _, m := range kept {
		if m.Scope == "韶关" && m.Dataset == "node_price_peak" {
			t.Fatalf("已确认的韶关档位不该被本次解析覆盖：%+v", m)
		}
	}

	if got := dropConfirmedIncoming(map[string]bool{}, incoming); len(got) != 3 {
		t.Fatalf("没有已确认行时应全量保留，实际 %d 条", len(got))
	}
}
