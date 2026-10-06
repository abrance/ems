package forecast

import (
	"math"
	"testing"

	"common/c_chart"
)

func TestMedianStepMinutes(t *testing.T) {
	if got := medianStepMinutes(nil); got != 15 {
		t.Errorf("空序列应返回默认 15，got %v", got)
	}
	if got := medianStepMinutes([]int64{1000}); got != 15 {
		t.Errorf("单点应返回默认 15，got %v", got)
	}

	// 15 分钟间隔
	times := []int64{0, 900000, 1800000, 2700000}
	if got := medianStepMinutes(times); math.Abs(got-15) > 1e-9 {
		t.Errorf("15 分钟间隔推断错误: %v", got)
	}

	// 1 分钟间隔
	times = []int64{0, 60000, 120000}
	if got := medianStepMinutes(times); math.Abs(got-1) > 1e-9 {
		t.Errorf("1 分钟间隔推断错误: %v", got)
	}

	// 存在缺口时取中位数，不受极端值影响
	times = []int64{0, 900000, 1800000, 1800000 + 3600000}
	if got := medianStepMinutes(times); math.Abs(got-15) > 1e-9 {
		t.Errorf("含缺口时应取中位数 15，got %v", got)
	}

	// 时间非递增
	if got := medianStepMinutes([]int64{5000, 1000}); got != 15 {
		t.Errorf("非递增应返回默认 15，got %v", got)
	}
}

func TestBuildForecastTimes(t *testing.T) {
	got := buildForecastTimes(1000000, 15, 3)
	want := []int64{1000000 + 900000, 1000000 + 1800000, 1000000 + 2700000}
	if len(got) != len(want) {
		t.Fatalf("长度 = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 点 = %d, want %d", i, got[i], want[i])
		}
	}
	if n := buildForecastTimes(0, 15, 0); len(n) != 0 {
		t.Errorf("0 步长应返回空，got %v", n)
	}
}

func TestParseChartPoints(t *testing.T) {
	if ts, vs, err := parseChartPoints(nil); err != nil || ts != nil || vs != nil {
		t.Errorf("nil chart 应返回空: %v %v %v", ts, vs, err)
	}

	chart := &c_chart.ChartData{
		XAxis:  c_chart.XAxis{Data: []string{"3000", "1000", "2000", "bad"}},
		Series: []*c_chart.Series{{Data: []string{"3.5", "1.5", "", "9"}}},
	}
	ts, vs, err := parseChartPoints(chart)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 乱序输入应被排序；空值与非法时间戳被跳过
	if len(ts) != 2 || len(vs) != 2 {
		t.Fatalf("点数 = %d/%d, want 2/2", len(ts), len(vs))
	}
	if ts[0] != 1000 || vs[0] != 1.5 || ts[1] != 3000 || vs[1] != 3.5 {
		t.Errorf("解析结果错误: ts=%v vs=%v", ts, vs)
	}
}
