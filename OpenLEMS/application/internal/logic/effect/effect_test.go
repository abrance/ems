package effect

import (
	"testing"

	"common/c_chart"
)

func TestChartToSeries(t *testing.T) {
	if got := chartToSeries(nil); len(got) != 0 {
		t.Errorf("nil chart 应为空，got %v", got)
	}
	if got := chartToSeries(&c_chart.ChartData{}); len(got) != 0 {
		t.Errorf("空 chart 应为空，got %v", got)
	}

	chart := &c_chart.ChartData{
		XAxis: c_chart.XAxis{Data: []string{"1000", "2000", "3000", "4000", "bad"}},
		Series: []*c_chart.Series{
			{Data: []string{"5.0", "", "7.5", "bad", "9"}},
		},
	}
	got := chartToSeries(chart)
	want := map[int64]float64{1000: 5.0, 3000: 7.5}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for ts, v := range want {
		if got[ts] != v {
			t.Errorf("got[%d] = %v, want %v", ts, got[ts], v)
		}
	}
}

func TestChartToSeriesLengthMismatch(t *testing.T) {
	// 时间戳少于数据时，以较短者为准，不越界
	chart := &c_chart.ChartData{
		XAxis:  c_chart.XAxis{Data: []string{"1000"}},
		Series: []*c_chart.Series{{Data: []string{"5.0", "6.0", "7.0"}}},
	}
	got := chartToSeries(chart)
	if len(got) != 1 || got[1000] != 5.0 {
		t.Errorf("长度不匹配处理错误: %v", got)
	}
}
