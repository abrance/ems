package log

import (
	"context"
	"strings"

	"common/c_log"
	"s_db"
	"s_db/s_db_basic"

	"github.com/gogf/gf/v2/os/glog"
)

// 全局日志级别：管**系统日志**（c_log.Info / Infof 这类，落到文件与控制台）。
//
// 配置里的 logger.level 是初值（默认 all），这里是运行期覆盖 ——
// 存在设置里，改完立即用 SetLevelStr 生效，不用重启。
const defaultGlobalLevel = "ALL"

// ApplyGlobalLogLevel 把「全局日志级别」设置应用到 GoFrame logger。
// 返回实际生效的级别串；值非法、级别未变时返回空串（glog 没有级别 → 字符串的反查，不硬凑）。
func ApplyGlobalLogLevel(ctx context.Context, logger *glog.Logger) string {
	if logger == nil {
		return defaultGlobalLevel
	}

	level := defaultGlobalLevel
	if v := s_db.GetSettingService().GetSettingValueBySystemSettingDefine(
		ctx, s_db_basic.SystemSettingLogLevelGlobal,
	); v != nil && strings.TrimSpace(*v) != "" {
		level = strings.TrimSpace(*v)
	}

	// 拿不到设置或值非法时**保持现状**，而不是把级别调低：
	// 日志级别配错时宁可多记，也不要因为一个手输的错值让整台设备的日志静默消失。
	if err := logger.SetLevelStr(level); err != nil {
		c_log.Warningf(ctx, "全局日志级别 %q 无效，保持原有级别不变: %v", level, err)
		return ""
	}
	c_log.Infof(ctx, "全局日志级别已应用: %s", level)
	return level
}
