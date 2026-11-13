package forecast

import (
	"testing"
	"time"

	"application/internal/logic/gdmetric"
)

// 回归用例：真值断了，回测必须自己知道「这批数字是哪天的」。
//
// 现场事故（2026-11-13 查证）：gd_metric.csv 的实际值停在 2026-10-02，
// 页面却按「近 7 天」锚到数据尾部，算出的是一个看起来像近期表现的数字。
// 所以滞后天数与陈旧标记必须由后端算、随结果下发，不靠前端猜。
func TestTruthFreshness(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	cases := []struct {
		name      string
		dataAsOf  string
		today     time.Time
		wantLag   int
		wantStale bool
	}{
		{"当天", "2026-11-13", time.Date(2026, 11, 13, 11, 44, 0, 0, loc), 0, false},
		{"阈值内 7 天", "2026-11-06", time.Date(2026, 11, 13, 0, 5, 0, 0, loc), 7, false},
		{"刚好超过 8 天", "2026-11-05", time.Date(2026, 11, 13, 0, 5, 0, 0, loc), 8, true},
		{"现场 42 天", "2026-10-02", time.Date(2026, 11, 13, 11, 44, 0, 0, loc), 42, true},
		// 空/坏日期 → 不知道多旧，必须按陈旧处理（不能把「不知道」当「新鲜」）
		{"日期为空", "", time.Date(2026, 11, 13, 0, 0, 0, 0, loc), -1, true},
		{"日期非法", "2026-13-40", time.Date(2026, 11, 13, 0, 0, 0, 0, loc), -1, true},
		// 时钟回拨/未来日期不出现负滞后
		{"数据日期在未来", "2026-11-20", time.Date(2026, 11, 13, 0, 0, 0, 0, loc), 0, false},
	}
	for _, c := range cases {
		lag, stale := truthFreshness(c.dataAsOf, c.today, loc)
		if lag != c.wantLag || stale != c.wantStale {
			t.Errorf("%s: got lag=%d stale=%v, want lag=%d stale=%v", c.name, lag, stale, c.wantLag, c.wantStale)
		}
	}
}

// 报「实际值截至哪天」要用「有实际值的最新日」，不是「96 点齐全的最新日」：
// CSV 最后一天常常少几个点（10-02 只有 92 点），说成 10-01 会把断点报早一天。
func TestLatestRealDateUsesAnyRealSample(t *testing.T) {
	series := []gdmetric.TimedSample{
		{Date: "2026-10-01", Time: "00:15", Value: 1, SourceID: gdmetric.SourceExchange},
		{Date: "2026-10-02", Time: "23:00", Value: 2, SourceID: gdmetric.SourceExchange},
	}
	if got := latestRealDate(series); got != "2026-10-02" {
		t.Fatalf("latestRealDate = %q, want 2026-10-02", got)
	}
	if got := latestRealDate(nil); got != "" {
		t.Fatalf("空序列应返回空字符串，got %q", got)
	}
}
