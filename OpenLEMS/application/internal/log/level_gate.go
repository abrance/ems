package log

import (
	"common/c_enum"
	"strings"
)

// 业务日志的级别过滤。
//
// 为什么要有这一层：业务日志（c_log.BizInfo / BizErrorf 这类）默认**全量入库**，
// protocol / device 的 DEBUG 会把库和日志文件刷得很快。原先只有两个特例开关
// （ems DEBUG、逐设备 DEBUG），没有「按模块定级别」的手段。
//
// 与那两个开关的关系：**先过级别，再看开关**。级别是模块粒度的一道闸，
// 开关是模块内的更细条件。两者都满足才记录 —— 页面要把这层关系写清楚，
// 否则会出现「我把 ems 设成 INFO 了，为什么 DEBUG 还是不入库」这种困惑，
// 反过来说「我把 ems 设回 DEBUG 了怎么还是不入库（因为系统调试日志开关没开）」也一样。

// levelRank 级别序号，越大越严重。
// 未知级别按 INFO 处理：宁可多记也不要因为写错一个值就把整类日志静默掉。
func levelRank(level string) int {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "DEBUG":
		return 10
	case "INFO":
		return 20
	case "WARN", "WARNING":
		return 30
	case "ERROR":
		return 40
	default:
		return 20
	}
}

// levelAllowed 该条日志是否达到配置的最低级别。
//
//	minLevel 为空 / ALL / DEV → 不过滤（配置缺失或写的是「全部」时的行为要保守）
//	否则 level >= minLevel 才记录
func levelAllowed(minLevel string, level c_enum.ELogLevel) bool {
	m := strings.ToUpper(strings.TrimSpace(minLevel))
	if m == "" || m == "ALL" || m == "DEV" || m == "DEVELOP" {
		return true
	}
	return levelRank(string(level)) >= levelRank(m)
}
