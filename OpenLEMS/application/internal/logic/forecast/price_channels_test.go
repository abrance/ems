package forecast

import (
	"testing"
	"time"
)

// 多变量输入的对齐规则：某个时刻缺任何一个通道的值，就把这个时刻从所有通道去掉。
// 通道错位比缺点严重得多 —— 那等于拿别的时刻的负荷当这一时刻的协变量。
func TestAlignChannelsDropsUnmatchedTimestamps(t *testing.T) {
	loc := time.UTC
	base := time.Date(2026, 9, 30, 0, 15, 0, 0, loc)
	times := make([]time.Time, 0, 5)
	values := make([]float64, 0, 5)
	for i := 0; i < 5; i++ {
		times = append(times, base.Add(time.Duration(i)*15*time.Minute))
		values = append(values, float64(100+i))
	}
	// 协变量 1：缺第 3 个时刻；协变量 2：缺第 1 个时刻
	cov1 := map[string]float64{}
	cov2 := map[string]float64{}
	for i, ts := range times {
		if i != 2 {
			cov1[sampleKey(ts)] = float64(1000 + i)
		}
		if i != 1 {
			cov2[sampleKey(ts)] = float64(5000 + i)
		}
	}

	keptTimes, matrix, dropped := alignChannels(times, values, []map[string]float64{cov1, cov2}, loc)
	if dropped != 2 || len(keptTimes) != 3 {
		t.Fatalf("应丢掉第 1、2 两个时刻: kept=%d dropped=%d", len(keptTimes), dropped)
	}
	if len(matrix) != 3 {
		t.Fatalf("通道数 = %d, want 3（目标序列 + 2 个协变量）", len(matrix))
	}
	for i := range matrix {
		if len(matrix[i]) != 3 {
			t.Fatalf("第 %d 个通道长度 = %d, 各通道必须等长", i, len(matrix[i]))
		}
	}
	// 保留的是第 0、3、4 个时刻（第 1 个被协变量 2 缺值丢掉、第 2 个被协变量 1 缺值丢掉），
	// 且各通道取同一时刻的值
	if matrix[0][1] != 103 || matrix[1][1] != 1003 || matrix[2][1] != 5003 {
		t.Fatalf("对齐结果错: %v", matrix)
	}
	if keptTimes[1].Format("15:04") != "01:00" {
		t.Fatalf("保留时刻错: %s", keptTimes[1])
	}
}

// 协变量完全对不上时，调用方要能发现「一个时刻都没留下」并退回单变量。
func TestAlignChannelsNoOverlap(t *testing.T) {
	loc := time.UTC
	base := time.Date(2026, 9, 30, 0, 15, 0, 0, loc)
	times := []time.Time{base, base.Add(15 * time.Minute)}
	kept, matrix, dropped := alignChannels(times, []float64{1, 2},
		[]map[string]float64{{sampleKey(base.Add(24 * time.Hour)): 7}}, loc)
	if len(kept) != 0 || dropped != 2 {
		t.Fatalf("应全部丢掉: kept=%d dropped=%d", len(kept), dropped)
	}
	if len(matrix[0]) != 0 {
		t.Fatalf("空结果不该留下值: %v", matrix)
	}
}

// 只有实时电价加协变量；日前预测实测无增益，保持单变量（别顺手给它也加上）。
func TestPriceCovariateKindsOnlyRealtime(t *testing.T) {
	if len(priceCovariateKinds["ahead"]) != 0 {
		t.Errorf("日前不该有协变量通道: %v", priceCovariateKinds["ahead"])
	}
	kinds := priceCovariateKinds["real"]
	if len(kinds) != 2 {
		t.Fatalf("实时应有 2 个协变量通道: %v", kinds)
	}
}
