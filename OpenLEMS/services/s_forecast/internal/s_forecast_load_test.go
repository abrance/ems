package internal

import (
	"testing"

	"common/c_chart"
)

func TestParseChartSeries(t *testing.T) {
	if got := parseChartSeries(nil); got != nil {
		t.Errorf("nil chart 应返回 nil，got %v", got)
	}
	if got := parseChartSeries(&c_chart.ChartData{}); got != nil {
		t.Errorf("空 chart 应返回 nil，got %v", got)
	}

	chart := &c_chart.ChartData{
		Series: []*c_chart.Series{
			{Data: []string{"1.00", "", "2.50", "bad", "3"}},
		},
	}
	got := parseChartSeries(chart)
	want := []float64{1.0, 2.5, 3.0}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
