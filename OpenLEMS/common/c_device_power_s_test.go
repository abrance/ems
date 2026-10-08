package common

import (
	"errors"
	"testing"

	"common/c_chart"
)

func chart(values ...string) *c_chart.ChartData {
	return &c_chart.ChartData{Series: []*c_chart.Series{{Name: "x", Data: values}}}
}

// 关键行为：按顺序试候选 Key，取第一个**有数据**的。
// （不存在的 Key 不报错，而是返回一个空点 —— 所以判断依据是「有没有非空值」）
func TestPickFirstWithValues(t *testing.T) {
	empty := chart("")
	one := chart("1.5")

	got, err := pickFirstWithValues([]string{"Power", "TotalActivePower"}, func(key string) (*c_chart.ChartData, error) {
		if key == "Power" {
			return empty, nil
		}
		return one, nil
	})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if got != one {
		t.Fatalf("应取到第二个候选（第一个只有空点）")
	}

	// 第一个就有数据时不该再试后面的
	calls := 0
	if _, err := pickFirstWithValues([]string{"Power", "TotalActivePower"}, func(string) (*c_chart.ChartData, error) {
		calls++
		return one, nil
	}); err != nil || calls != 1 {
		t.Fatalf("第一个有数据时应只查一次，实际 %d 次 err=%v", calls, err)
	}
}

func TestPickFirstWithValuesNoData(t *testing.T) {
	// 全部候选都没有数据 → 空结果 + 无错误（调用方据此走空态）
	for _, c := range []*c_chart.ChartData{nil, {}, chart(), chart("", "  ")} {
		got, err := pickFirstWithValues([]string{"Power"}, func(string) (*c_chart.ChartData, error) { return c, nil })
		if err != nil || got != nil {
			t.Fatalf("没有任何数据时应返回 (nil, nil)，实际 (%v, %v)", got, err)
		}
	}
}

func TestPickFirstWithValuesErrors(t *testing.T) {
	// 查询报错 → 继续试下一个；全都报错 → 把最后一个错误带出来（不能静默成「没数据」）
	boom := errors.New("db down")
	if _, err := pickFirstWithValues([]string{"Power", "TotalActivePower"}, func(string) (*c_chart.ChartData, error) {
		return nil, boom
	}); err == nil {
		t.Fatal("全部报错时应把错误带出来")
	}
	got, err := pickFirstWithValues([]string{"Power", "TotalActivePower"}, func(key string) (*c_chart.ChartData, error) {
		if key == "Power" {
			return nil, boom
		}
		return chart("2"), nil
	})
	if err != nil || got == nil {
		t.Fatalf("前一个报错、后一个有数据时应成功，实际 (%v, %v)", got, err)
	}
}

func TestChartHasValues(t *testing.T) {
	if chartHasValues(nil) || chartHasValues(&c_chart.ChartData{}) {
		t.Fatal("空图表应判为没有数据")
	}
	if chartHasValues(chart("", "")) {
		t.Fatal("只有空点的图表应判为没有数据")
	}
	if !chartHasValues(chart("", "0")) {
		t.Fatal("有一个非空值就该判为有数据（0 也是数据）")
	}
}
