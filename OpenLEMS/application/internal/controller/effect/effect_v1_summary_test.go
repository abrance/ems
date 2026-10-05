package effect

import (
	"testing"
	"time"
)

func TestParseDateRange(t *testing.T) {
	// 缺省为今日 00:00 ~ 次日 00:00
	start, end, err := parseDateRange("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	now := time.Now()
	wantStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if !start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", start, wantStart)
	}
	if !end.Equal(wantStart.AddDate(0, 0, 1)) {
		t.Errorf("end = %v, want %v", end, wantStart.AddDate(0, 0, 1))
	}

	// 指定区间：结束日应含当日，即 +1 天
	start, end, err = parseDateRange("2026-10-01", "2026-10-03")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if start.Format("2006-01-02") != "2026-10-01" {
		t.Errorf("start = %v", start)
	}
	if end.Format("2006-01-02") != "2026-10-04" {
		t.Errorf("end = %v, want 2026-10-04（含结束当日）", end)
	}

	// 非法日期
	if _, _, err := parseDateRange("2026/10/01", ""); err == nil {
		t.Error("非法开始日期应报错")
	}
	if _, _, err := parseDateRange("", "bad"); err == nil {
		t.Error("非法结束日期应报错")
	}
}
