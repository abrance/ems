package effect

import (
	"testing"

	"common/c_chart"
	"s_effect"
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

func TestSeriesRange(t *testing.T) {
	// 空序列 → 0,0（页面上据此显示「无数据」，不能当成时间戳 0 去格式化）
	if from, to := seriesRange(nil); from != 0 || to != 0 {
		t.Fatalf("空序列应为 0,0，得到 %d,%d", from, to)
	}
	// 单个点
	if from, to := seriesRange(s_effect.SSeries{1500: 1}); from != 1500 || to != 1500 {
		t.Fatalf("单点应为首尾相同，得到 %d,%d", from, to)
	}
	// 乱序也要取到正确的最早/最晚（map 遍历顺序是随机的）
	got := s_effect.SSeries{3000: 1, 1000: 2, 2000: 3, 500: 4}
	for i := 0; i < 20; i++ {
		from, to := seriesRange(got)
		if from != 500 || to != 3000 {
			t.Fatalf("第 %d 次：应为 500,3000，得到 %d,%d", i, from, to)
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
