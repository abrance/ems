package forecast

import "testing"

// 留痕对账的口径细节：目标时刻可能是「日」也可能是「时刻」，配对真值时都要取日期部分。
func TestFromDate(t *testing.T) {
	cases := map[string]string{
		"2026-09-24":        "2026-09-24",
		"2026-09-20 00:00":  "2026-09-20",
		"2026-09-20 23:45 ": "2026-09-20",
		"":                  "",
	}
	for in, want := range cases {
		if got := fromDate(in); got != want {
			t.Errorf("fromDate(%q) = %q, want %q", in, got, want)
		}
	}
}
