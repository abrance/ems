// Package retention 数据保留：按三条设置清理历史数据。
//
// 为什么需要：`DeviceRetentionDays` / `SystemRetentionDays` / `LogRetentionDays`
// 三个设置原先**只写不读** —— 界面上能改、接口会成功，但没有任何代码去清理，
// 存储层 `promtsdb.Open(path, nil, nil, nil)` 也没配保留（注释写着「由调用方做外部清理」）。
// 一台长期运行的设备会就这样把磁盘填满。这里让它真的生效。
package retention

import (
	"context"
	"sync"
	"time"

	"common"
	"common/c_base"
	"common/c_log"
	"s_db"
	"s_db/s_db_basic"
	"strconv"
	"strings"

	"github.com/shockerli/cvt"
)

// DefaultInterval 自动清理间隔。
//
// 定成一天：设置的单位是天，跑得比这更勤没有意义；而每次清理末尾会做一次
// tsdb 的 tombstone 整理（重写数据块），那是真有 I/O 代价的。
const DefaultInterval = 24 * time.Hour

// SResult 一次清理的结果。记下来是为了能在界面上回答「上次跑了没有、删了什么」。
type SResult struct {
	At             time.Time
	DeviceDays     int
	SystemDays     int
	LogDays        int
	DeviceBefore   time.Time
	SystemBefore   time.Time
	LogBefore      time.Time
	LogRowsDeleted int
	TimeMs         int64
	Err            string
}

var (
	lastMu sync.RWMutex
	last   *SResult
)

// Last 上一次清理结果；本次启动后没跑过则返回 nil。
func Last() *SResult {
	lastMu.RLock()
	defer lastMu.RUnlock()
	return last
}

func setLast(r *SResult) {
	lastMu.Lock()
	last = r
	lastMu.Unlock()
}

// ParseDays 解析设置里存的保留天数。
//
// 只接受纯数字，空 / 0 / 负数 / 带单位（"30天"）一律回退到 fallback ——
// **回退而不是「不清理」**：读不到就永远不清理，磁盘会慢慢涨满，
// 那比用一个保守的默认值清理更糟。
func ParseDays(value string, fallback int) int {
	v := strings.TrimSpace(value)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// SettingDays 读保留天数设置；缺失或非法时回退到定义里的默认值。
func SettingDays(ctx context.Context, def *s_db_basic.SSystemSettingDefine) int {
	fallback := cvt.Int(def.DefaultValue)
	if v := s_db.GetSettingService().GetSettingValueBySystemSettingDefine(ctx, def); v != nil {
		return ParseDays(*v, fallback)
	}
	return fallback
}

// RunOnce 按当前设置清理一次。返回结果（含错误信息，不吞掉）。
func RunOnce(ctx context.Context) *SResult {
	start := time.Now()
	res := &SResult{At: start}

	res.DeviceDays = SettingDays(ctx, s_db_basic.SystemSettingDeviceRetentionDays)
	res.SystemDays = SettingDays(ctx, s_db_basic.SystemSettingSystemRetentionDays)
	res.LogDays = SettingDays(ctx, s_db_basic.SystemSettingLogRetentionDays)
	res.DeviceBefore = start.AddDate(0, 0, -res.DeviceDays)
	res.SystemBefore = start.AddDate(0, 0, -res.SystemDays)
	res.LogBefore = start.AddDate(0, 0, -res.LogDays)

	// 用 OrNil：存储没起来时（比如某些测试/子命令）不该 panic，只跳过并说明
	storage := common.GetStorageInstanceOrNil()
	if storage == nil {
		res.Err = "存储未初始化，跳过历史数据清理"
	} else {
		// 协议指标算设备侧数据（它也是设备/协议采集来的），跟着设备保留天数走 ——
		// 这一点要写进文档，否则用户不知道协议数据归谁管。
		if err := storage.DeleteBefore(
			res.DeviceBefore,
			c_base.StorageTypeDevice, c_base.StorageTypeProtocol,
		); err != nil {
			res.Err = "清理设备数据失败: " + err.Error()
		}
		if err := storage.DeleteBefore(res.SystemBefore, c_base.StorageTypeSystem); err != nil {
			if res.Err != "" {
				res.Err += "; "
			}
			res.Err += "清理系统数据失败: " + err.Error()
		}
	}

	if deleted, err := s_db.GetLogService().DeleteLogBefore(ctx, res.LogBefore); err != nil {
		if res.Err != "" {
			res.Err += "; "
		}
		res.Err += "清理日志失败: " + err.Error()
	} else {
		res.LogRowsDeleted = deleted
	}

	res.TimeMs = time.Since(start).Milliseconds()
	setLast(res)

	if res.Err != "" {
		c_log.BizWarningf(ctx, "数据保留清理部分失败 - 设备保留 %d 天/系统 %d 天/日志 %d 天，错误: %s",
			res.DeviceDays, res.SystemDays, res.LogDays, res.Err)
	} else {
		c_log.BizInfof(ctx, "数据保留清理完成 - 设备保留 %d 天/系统 %d 天/日志 %d 天，日志删除 %d 行，耗时 %dms",
			res.DeviceDays, res.SystemDays, res.LogDays, res.LogRowsDeleted, res.TimeMs)
	}
	return res
}

// Start 启动定时清理。首次延迟一会儿再跑，避免与启动过程抢 I/O；之后按 interval 周期跑。
func Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultInterval
	}
	go func() {
		timer := time.NewTimer(5 * time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				RunOnce(ctx)
				timer.Reset(interval)
			}
		}
	}()
	c_log.Infof(ctx, "数据保留定时清理已启动：每 %s 一次（首次 5 分钟后）", interval)
}
