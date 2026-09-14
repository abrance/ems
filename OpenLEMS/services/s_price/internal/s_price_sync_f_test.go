package internal

import (
	"testing"

	"common/c_enum"

	"github.com/gogf/gf/v2/encoding/gjson"
)

// 广东珠三角五市 2026-07 真实响应，用于校验解析逻辑（含 stage 为 JSON 字符串的情况）
const guangdongJulyRecord = `{
  "regionId": 40, "startTime": 1782835200, "endTime": 1785513599,
  "usage": 24, "cost": 12, "voltage": 19,
  "top": "1.45086875", "peak": "1.16616875", "normal": "0.69736875",
  "valley": "0.28216875", "deep": "",
  "stage": "{\"deep\":\"[]\",\"normal\":\"[\\\"08:00-10:00\\\",\\\"12:00-14:00\\\",\\\"19:00-24:00\\\"]\",\"peak\":\"[\\\"10:00-11:00\\\",\\\"14:00-15:00\\\",\\\"17:00-19:00\\\"]\",\"top\":\"[\\\"11:00-12:00\\\",\\\"15:00-17:00\\\"]\",\"valley\":\"[\\\"00:00-08:00\\\"]\"}",
  "policyUuid": "d54e935a-9dd1-11f0-850d-46a1f660a16d"
}`

func TestBuildPriceModelGuangdong(t *testing.T) {
	record := gjson.New(guangdongJulyRecord)
	cfg := priceSyncConfig{Priority: 1, Description: "电网分时电价"}

	model, err := buildPriceModel(cfg, record)
	if err != nil {
		t.Fatalf("buildPriceModel failed: %v", err)
	}

	if model.RemoteId != "energydc:d54e935a-9dd1-11f0-850d-46a1f660a16d" {
		t.Errorf("unexpected remoteId: %s", model.RemoteId)
	}

	dateRange := &SDateRange{}
	if err := gjson.DecodeTo(model.DateRange, dateRange); err != nil {
		t.Fatalf("decode dateRange failed: %v", err)
	}
	if dateRange.StartDate != "2026-07-01" {
		t.Errorf("unexpected startDate: %s", dateRange.StartDate)
	}
	if !dateRange.IsLongTerm {
		t.Error("expected IsLongTerm=true")
	}

	segments := []*SPriceSegment{}
	if err := gjson.DecodeTo(model.PriceSegments, &segments); err != nil {
		t.Fatalf("decode segments failed: %v", err)
	}

	expected := map[string]struct {
		price     float64
		priceType c_enum.EPriceType
	}{
		"00:00": {0.28216875, c_enum.EPriceTypeValley},
		"08:00": {0.69736875, c_enum.EPriceTypeFlat},
		"10:00": {1.16616875, c_enum.EPriceTypePeak},
		"11:00": {1.45086875, c_enum.EPriceTypeSharp},
		"12:00": {0.69736875, c_enum.EPriceTypeFlat},
		"14:00": {1.16616875, c_enum.EPriceTypePeak},
		"15:00": {1.45086875, c_enum.EPriceTypeSharp},
		"17:00": {1.16616875, c_enum.EPriceTypePeak},
		"19:00": {0.69736875, c_enum.EPriceTypeFlat},
	}
	if len(segments) != len(expected) {
		t.Fatalf("expected %d segments, got %d: %+v", len(expected), len(segments), segments)
	}
	for _, segment := range segments {
		want, ok := expected[segment.StartTime]
		if !ok {
			t.Errorf("unexpected segment start: %s", segment.StartTime)
			continue
		}
		if segment.Price != want.price || segment.PriceType != want.priceType {
			t.Errorf("segment %s mismatch: got price=%v type=%v, want price=%v type=%v",
				segment.StartTime, segment.Price, segment.PriceType, want.price, want.priceType)
		}
	}
}

func TestParseTimeRangesHandlesArrayAndString(t *testing.T) {
	arrayForm := gjson.New(`{"v":["00:00-08:00","19:00-24:00"]}`).Get("v")
	if got := parseTimeRanges(arrayForm); len(got) != 2 || got[1] != "19:00-24:00" {
		t.Errorf("array form failed: %+v", got)
	}

	stringForm := gjson.New(`{"v":"[\"00:00-08:00\"]"}`).Get("v")
	if got := parseTimeRanges(stringForm); len(got) != 1 || got[0] != "00:00-08:00" {
		t.Errorf("string form failed: %+v", got)
	}

	if got := parseTimeRanges(gjson.New(`{"v":"[]"}`).Get("v")); len(got) != 0 {
		t.Errorf("empty form failed: %+v", got)
	}
}
