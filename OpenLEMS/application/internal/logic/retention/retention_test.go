package retention

import "testing"

// 解析失败必须**回退到默认值**，不能变成 0 或「不清理」：
// 一个手输的错值（空、带单位、hea 负数）不该让清理彻底停摆。
func TestParseDays(t *testing.T) {
	cases := []struct {
		value    string
		fallback int
		want     int
	}{
		{"30", 30, 30},
		{" 90 ", 30, 90},
		{"1", 30, 1},
		{"", 30, 30},
		{"0", 30, 30}, // 0 天 = 删光，不能这么理解
		{"-5", 30, 30},
		{"abc", 30, 30},
		{"30天", 30, 30}, // 带单位不认，回退而不是当 30
		{"3.5", 30, 30},
		{"999", 30, 999},
		{"", 90, 90}, // fallback 用设置定义里的默认值
	}
	for _, c := range cases {
		if got := ParseDays(c.value, c.fallback); got != c.want {
			t.Errorf("ParseDays(%q, %d) = %d, 期望 %d", c.value, c.fallback, got, c.want)
		}
	}
}
