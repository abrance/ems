package log

import (
	"common"
	"common/c_enum"
	"common/c_log"
	"context"
	"fmt"
	"s_db"
	"s_db/s_db_basic"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/shockerli/cvt"
)

// BizLoggerAdapter 业务日志适配器：只输出到标准输出，云上由容器运行时收走（kubectl logs / docker logs）。
//
// 以前这里还会把每一行业务日志 insert 进 log 表（每行单开一个 goroutine），
// 上云后那份数据没人看、还要靠保留任务删；日志本来就该进容器日志。
// 保留下来的只有级别/开关这道闸 —— 用来防 protocol/device 的 DEBUG 刷屏。
type BizLoggerAdapter struct {
	out c_log.ILogger
}

// NewBizAdapter 创建业务日志适配器
func NewBizAdapter() c_log.ILogger {
	return &BizLoggerAdapter{out: NewSystemAdapter(g.Log())}
}

// getLogInfo 从上下文中提取日志类型与设备ID
func getLogInfo(ctx context.Context) (logType c_enum.ESystemGroupType, deviceId string) {
	if ctx == nil {
		return c_enum.ELogTypeEms, ""
	}
	// 按优先级检查上下文键，直接使用枚举值作为键
	if v := ctx.Value(c_enum.ELogTypeDevice); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return c_enum.ELogTypeDevice, s
		}
	}
	if v := ctx.Value(c_enum.ELogTypeProtocol); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return c_enum.ELogTypeProtocol, s
		}
	}
	if v := ctx.Value(c_enum.ELogTypePolicy); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return c_enum.ELogTypePolicy, s
		}
	}
	if v := ctx.Value(c_enum.ELogTypeRemote); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return c_enum.ELogTypeRemote, s
		}
	}
	return c_enum.ELogTypeEms, ""
}

// allow 该条日志是否输出：
//   - 模块级别闸（设置里按日志类型配置的最低级别，改完立即生效）
//   - ems 的 DEBUG 需要打开系统调试日志开关
//   - device 的 DEBUG 需要该设备打开调试模式
func (b *BizLoggerAdapter) allow(ctx context.Context, level c_enum.ELogLevel, logType c_enum.ESystemGroupType, deviceId string) bool {
	if def := s_db_basic.LogLevelSettingFor(logType); def != nil {
		if v := s_db.GetSettingService().GetSettingValueBySystemSettingDefine(ctx, def); v != nil {
			if !levelAllowed(*v, level) {
				return false
			}
		}
	}

	if level != c_enum.Debug {
		return true
	}

	if logType == c_enum.ELogTypeEms {
		v := s_db.GetSettingService().GetSettingValueBySystemSettingDefine(
			ctx, s_db_basic.SystemSettingSystemEnableDebugLog,
		)
		return v != nil && cvt.Bool(*v)
	}

	if logType == c_enum.ELogTypeDevice && deviceId != "" {
		deviceConfig := common.GetDeviceManager().GetDeviceConfigById(deviceId)
		return deviceConfig == nil || deviceConfig.EnableDebug
	}

	return true
}

func (b *BizLoggerAdapter) write(ctx context.Context, level c_enum.ELogLevel, content string) {
	if content == "" {
		return
	}
	logType, deviceId := getLogInfo(ctx)
	if !b.allow(ctx, level, logType, deviceId) {
		return
	}
	content = "BIZ ====> " + content
	switch level {
	case c_enum.Debug:
		b.out.Debug(ctx, content)
	case c_enum.Info:
		b.out.Info(ctx, content)
	case c_enum.Warn:
		b.out.Warning(ctx, content)
	case c_enum.Error:
		b.out.Error(ctx, content)
	default:
		b.out.Info(ctx, content)
	}
}

func (b *BizLoggerAdapter) Debug(ctx context.Context, v ...interface{}) {
	b.write(ctx, c_enum.Debug, fmt.Sprint(v...))
}

func (b *BizLoggerAdapter) Debugf(ctx context.Context, format string, v ...interface{}) {
	b.write(ctx, c_enum.Debug, fmt.Sprintf(format, v...))
}

func (b *BizLoggerAdapter) Info(ctx context.Context, v ...interface{}) {
	b.write(ctx, c_enum.Info, fmt.Sprint(v...))
}

func (b *BizLoggerAdapter) Infof(ctx context.Context, format string, v ...interface{}) {
	b.write(ctx, c_enum.Info, fmt.Sprintf(format, v...))
}

func (b *BizLoggerAdapter) Warning(ctx context.Context, v ...interface{}) {
	b.write(ctx, c_enum.Warn, fmt.Sprint(v...))
}

func (b *BizLoggerAdapter) Warningf(ctx context.Context, format string, v ...interface{}) {
	b.write(ctx, c_enum.Warn, fmt.Sprintf(format, v...))
}

func (b *BizLoggerAdapter) Error(ctx context.Context, v ...interface{}) {
	b.write(ctx, c_enum.Error, fmt.Sprint(v...))
}

func (b *BizLoggerAdapter) Errorf(ctx context.Context, format string, v ...interface{}) {
	b.write(ctx, c_enum.Error, fmt.Sprintf(format, v...))
}
