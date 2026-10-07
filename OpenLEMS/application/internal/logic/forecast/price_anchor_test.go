package forecast

import (
	"testing"
	"time"
)

// 这个 bug 的现象：部署上「96 点电价预测」报
// 「15 分钟日前电价数据不足（0 点，至少需要一天 96 点）」，
// 而 /api/gd-metric 明明有数据 —— 因为 CSV 只到 08-31，代码却拿「今天」(10-07) 当锚点，
// 取的窗口整段为空。
func TestClampAnchor(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	today := time.Date(2026, 10, 7, 15, 30, 0, 0, loc)

	cases := []struct {
		name       string
		latestDate string
		want       string
	}{
		{"数据滞后一个多月 → 收到最新可用日", "2026-08-31", "2026-08-31"},
		{"数据只滞后一天 → 也收一天", "2026-10-06", "2026-10-06"},
		{"数据就是今天 → 不动", "2026-10-07", "2026-10-07"},
		{"数据比今天新（不该发生）→ 不动", "2026-10-20", "2026-10-07"},
		{"拿不到最新日期 → 用今天", "", "2026-10-07"},
		{"日期格式不对 → 用今天", "2026/08/31", "2026-10-07"},
		{"垃圾值 → 用今天", "not-a-date", "2026-10-07"},
	}
	for _, c := range cases {
		// 只比日期：锚点校正管的是「哪一天」，保留原有时刻（预测起点仍按当天算）
		got := clampAnchor(today, c.latestDate).Format(priceDateLayout)
		if got != c.want {
			t.Errorf("%s: clampAnchor(%s, %q) = %s, 期望 %s",
				c.name, today.Format(priceDateLayout), c.latestDate, got, c.want)
		}
	}
}
