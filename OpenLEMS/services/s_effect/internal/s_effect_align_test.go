package internal

import "testing"

func TestBuildIntervals(t *testing.T) {
	if got := BuildIntervals(nil, nil, nil); got != nil {
		t.Errorf("空输入应返回 nil，got %v", got)
	}

	// 只在交集时间戳上生成，且按时间升序
	grid := SSeries{3000: 30, 1000: 10, 2000: 20}
	ess := SSeries{1000: 1, 2000: -2, 4000: 4}
	got := BuildIntervals(grid, ess, nil)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (%+v)", len(got), got)
	}
	if got[0].Timestamp != 1000 || got[1].Timestamp != 2000 {
		t.Errorf("时间戳未升序或未取交集: %+v", got)
	}
	if got[0].GridPower != 10 || got[0].EssPower != 1 {
		t.Errorf("点位0 值错误: %+v", got[0])
	}
	if got[1].GridPower != 20 || got[1].EssPower != -2 {
		t.Errorf("点位1 值错误: %+v", got[1])
	}
}

func TestBuildIntervalsPriceAt(t *testing.T) {
	grid := SSeries{1000: 10}
	ess := SSeries{1000: 1}
	got := BuildIntervals(grid, ess, func(ts int64) float64 { return float64(ts) / 1000 })
	if len(got) != 1 || got[0].Price != 1 {
		t.Fatalf("priceAt 未生效: %+v", got)
	}
}
