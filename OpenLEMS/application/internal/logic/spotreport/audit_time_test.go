package spotreport

import (
	"testing"
	"time"

	"github.com/gogf/gf/v2/os/gtime"

	"s_db/s_db_model"
)

// 审计时间的两种库内写法必须显示成同一个北京时间：
//   - 带偏移（ORM / 老 OCR 流水）：按自身偏移换算
//   - 不带偏移（pm-api 导入脚本写本地墙钟）：数字就是北京时间，不能再加 8 小时
//
// 曾经的 bug：无偏移的 '2026-11-11 16:27:21' 被 sqlite 驱动当 UTC，页面显示成 2026-11-12T00:27。
func TestAuditCreatedAtNormalizesNaiveLocalTime(t *testing.T) {
	// sqlite 驱动把无偏移文本按 UTC 解析，CreatedAt 会是 16:27Z（这正是多 8 小时的来源）
	naive := &s_db_model.SSpotMetricAuditModel{
		CreatedAt:     gtime.NewFromTime(time.Date(2026, 11, 11, 16, 27, 21, 0, time.UTC)),
		CreatedAtText: "2026-11-11 16:27:21",
	}
	if got, want := auditCreatedAt(naive), "2026-11-11T16:27:21+08:00"; got != want {
		t.Fatalf("无偏移时间应按北京时间解释: got %s want %s", got, want)
	}

	// 无偏移但带小数秒：同理按北京时间解释，小数秒被丢弃
	naiveFractional := &s_db_model.SSpotMetricAuditModel{
		CreatedAt:     gtime.NewFromTime(time.Date(2026, 11, 11, 16, 27, 21, 0, time.UTC)),
		CreatedAtText: "2026-11-11 16:27:21.123",
	}
	if got, want := auditCreatedAt(naiveFractional), "2026-11-11T16:27:21+08:00"; got != want {
		t.Fatalf("无偏移带小数秒的时间应按北京时间解释: got %s want %s", got, want)
	}

	// 带偏移的行：驱动按 12:33Z 解析，换算成北京时间应为 20:33
	zoned := &s_db_model.SSpotMetricAuditModel{
		CreatedAt:     gtime.NewFromTime(time.Date(2026, 9, 22, 12, 33, 42, 0, time.UTC)),
		CreatedAtText: "2026-09-22 12:33:42.375728332+00:00",
	}
	if got, want := auditCreatedAt(zoned), "2026-09-22T20:33:42+08:00"; got != want {
		t.Fatalf("带偏移时间应按自身偏移换算: got %s want %s", got, want)
	}
}

func TestHasZoneOffset(t *testing.T) {
	for s, want := range map[string]bool{
		"2026-11-11 16:27:21":                 false,
		"2026-09-22 12:33:42.375728332+00:00": true,
		"2026-09-22T12:33:42Z":                true,
		"2026-09-22 12:33:42+0800":            true,
	} {
		if got := hasZoneOffset(s); got != want {
			t.Fatalf("hasZoneOffset(%q) = %v, want %v", s, got, want)
		}
	}
}
