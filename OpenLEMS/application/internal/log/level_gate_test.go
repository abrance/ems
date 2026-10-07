package log

import (
	"testing"

	"common/c_enum"
)

// 默认值必须保持「不过滤」：加这个功能之前业务日志是全量入库的，
// 默认值一旦写成 INFO，现网就会静默少掉一大类日志。
func TestLevelAllowedDefaults(t *testing.T) {
	for _, minLevel := range []string{"", "ALL", "DEV", "DEVELOP", "  all  "} {
		if !levelAllowed(minLevel, c_enum.Debug) {
			t.Errorf("minLevel=%q 时应不过滤，但 DEBUG 被滤掉了", minLevel)
		}
	}
}

func TestLevelAllowed(t *testing.T) {
	cases := []struct {
		min   string
		level c_enum.ELogLevel
		want  bool
	}{
		{"DEBUG", c_enum.Debug, true},
		{"INFO", c_enum.Debug, false},
		{"INFO", c_enum.Info, true},
		{"INFO", c_enum.Warn, true},
		{"WARN", c_enum.Info, false},
		{"WARN", c_enum.Warn, true},
		{"WARN", c_enum.Error, true},
		{"ERROR", c_enum.Warn, false},
		{"ERROR", c_enum.Error, true},
		// 小写、空格要能容忍（用户在设置里手输）
		{" warn ", c_enum.Warn, true},
		{" warn ", c_enum.Info, false},
		// 未知级别按 INFO：宁多记，也别因为写错一个值把整类日志静默掉
		{"BOGUS", c_enum.Info, true},
		{"BOGUS", c_enum.Debug, false},
	}
	for _, c := range cases {
		if got := levelAllowed(c.min, c.level); got != c.want {
			t.Errorf("levelAllowed(%q, %s) = %v, 期望 %v", c.min, c.level, got, c.want)
		}
	}
}
