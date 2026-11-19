// Package retention 数据保留：按三条设置清理历史数据。
//
// 为什么需要：`DeviceRetentionDays` / `SystemRetentionDays` / `LogRetentionDays`
// 三个设置原先**只写不读** —— 界面上能改、接口会成功，但没有任何代码去清理，
// 存储层 `promtsdb.Open(path, nil, nil, nil)` 也没配保留（注释写着「由调用方做外部清理」）。
// 一台长期运行的设备会就这样把磁盘填满。这里让它真的生效。
package retention

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"common"
	"common/c_base"
	"common/c_log"
	"s_db"
	"s_db/s_db_basic"
	"strconv"
	"strings"

	"github.com/gogf/gf/v2/frame/g"
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
	LogBefore time.Time
	// 磁盘上 out/logs/app/*.log 的清理结果
	LogFilesDeleted int
	LogBytesFreed   int64
	TimeMs          int64
	Err             string
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

	// 业务日志只进容器标准输出，库里的 log 表已删除，这里只剩磁盘上的 rotate 文件要清：
	// GF 自带的 rotateBackupLimit 按「份数」管，管不到按天命名的历史文件，份数上限本身也很大
	// （100 份 × 5MB = 500MB），一次刷屏就能顶满。
	// 按 LogBefore 补一条时间维度，LogRetentionDays 才真的对磁盘生效。
	logDir := strings.TrimSpace(g.Cfg().MustGet(ctx, "logger.path").String())
	logFilePattern := g.Cfg().MustGet(ctx, "logger.file", "{Ymd}.log").String()
	files, freed, notes := CleanLogFiles(logDir, logFilePattern, res.LogBefore)
	res.LogFilesDeleted, res.LogBytesFreed = files, freed
	if len(notes) > 0 {
		if res.Err != "" {
			res.Err += "; "
		}
		res.Err += strings.Join(notes, "; ")
	}

	res.TimeMs = time.Since(start).Milliseconds()
	setLast(res)

	if res.Err != "" {
		c_log.BizWarningf(ctx, "数据保留清理部分失败 - 设备保留 %d 天/系统 %d 天/日志 %d 天，日志文件删除 %d 个（释放 %d 字节），错误: %s",
			res.DeviceDays, res.SystemDays, res.LogDays, res.LogFilesDeleted, res.LogBytesFreed, res.Err)
	} else {
		c_log.BizInfof(ctx, "数据保留清理完成 - 设备保留 %d 天/系统 %d 天/日志 %d 天，日志文件删除 %d 个（释放 %d 字节），耗时 %dms",
			res.DeviceDays, res.SystemDays, res.LogDays, res.LogFilesDeleted, res.LogBytesFreed, res.TimeMs)
	}
	return res
}

// CleanLogFiles 删除 dir 下 mtime 早于 before 的滚动日志文件，返回删除个数、释放字节数与非致命说明。
//
// 三条安全边界：
//   - 只认「8 位日期开头 + .log 结尾」的文件（{Ymd}.log 的产物），不碰 .csv / db.sqlite3* / ptdb；
//   - 永远跳过最新的那个文件 —— 它是当前正在写的，删了会丢日志；
//   - 目录缺失/不可读只返回说明，不算失败（容器首启、日志改到别处都可能出现）。
func CleanLogFiles(dir, filePattern string, before time.Time) (files int, bytesFreed int64, notes []string) {
	if dir == "" {
		return 0, 0, []string{"未配置 logger.path，跳过日志文件清理"}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0, []string{"日志目录 " + dir + " 不可读，跳过日志文件清理: " + err.Error()}
	}

	type logFile struct {
		name string
		size int64
	}
	var candidates []logFile
	newest := ""
	var newestMod time.Time
	for _, e := range entries {
		if e.IsDir() || !isAppLogFileName(e.Name(), filePattern) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if newest == "" || info.ModTime().After(newestMod) {
			newest, newestMod = e.Name(), info.ModTime()
		}
		if info.ModTime().Before(before) {
			candidates = append(candidates, logFile{name: e.Name(), size: info.Size()})
		}
	}

	for _, f := range candidates {
		if f.name == newest {
			continue
		}
		if err := os.Remove(filepath.Join(dir, f.name)); err != nil {
			notes = append(notes, "删除日志文件 "+f.name+" 失败: "+err.Error())
			continue
		}
		files++
		bytesFreed += f.size
	}
	return files, bytesFreed, notes
}

// isAppLogFileName 判断是否是本项目 `{Ymd}.log` 产生的滚动日志。
// 命名形如 20261111.20261111162721.123456.123.log，所以按「8 位数字日期 + .log」认，
// 比按前缀字符串匹配稳（{Ymd} 在模板开头时没有字面前缀可用）。
func isAppLogFileName(name, filePattern string) bool {
	if !strings.HasSuffix(name, ".log") {
		return false
	}
	if !strings.HasPrefix(strings.TrimSpace(filePattern), "{Ymd}") {
		return true
	}
	if len(name) < 8+len(".log") {
		return false
	}
	for _, c := range name[:8] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
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
