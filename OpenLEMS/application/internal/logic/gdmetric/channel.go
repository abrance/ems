package gdmetric

import (
	"context"
	"time"
)

// 通道退役守卫。
//
// 背景（2026-11-13 定案）：广东市场的权威数据已经改成官方披露接口直采（入库
// gd_spot_metric，见 tools/gdpm）。本包读的 gd_metric.csv 是第三方快照，
// 2026-11-11 起主动停更（config gd_metric.enabled=false），实际出清价止于 2026-10-02。
//
// 用户的硬规则：官方覆盖的能力一律读官方，CSV 路径直接删；官方没覆盖的（96 点分时、
// 96 点负荷、相似日、供给曲线）不许拿 CSV 假装还在工作，要明说用不了。
//
// 所以本文件把这层守卫放在**包的公开入口**上，而不是各个 controller 里各写一遍：
//   - 默认入口一律返回 ChannelRetiredError（带「止于哪天」），调用方不可能静默拿到旧 CSV；
//   - 只有显式名字带 Frozen 的入口才读 CSV，那是「历史演示」的 opt-in 通道，
//     调用点一眼能看出「这里在用一个冻结的数据源」。
//
// 以后要恢复 96 点，正确做法是接入官方 96 点通道后再把守卫摘掉，
// 而不是把这里的判断删掉让 CSV 悄悄回来。
const (
	// ChannelRetiredDate 最后一天有实际值（sourceId=9）的日期，按现场核对写死。
	ChannelRetiredDate = "2026-10-02"
	// ChannelRetiredReason 给用户看的停更原因（页面/接口原样展示）
	ChannelRetiredReason = "第三方分时通道（gd_metric.csv）已停更：官方披露接口只有日频，" +
		"96 点分时数据自 2026-10-02 起没有实际值；日频请用官方直采（/api/spot-market/board）"
)

// ChannelRetiredError 读了一个已停更的通道。
//
// DataAsOf 一定是具体日期（不是空串）：页面要把「数据止于哪天」说清楚，
// 含糊的空值会让人以为「暂时取不到」，而事实是「这个源不再更新」。
type ChannelRetiredError struct {
	// What 被请求的能力，例如「96 点电价序列」「供给曲线」
	What string
	// DataAsOf 该通道最后有实际值的日期 YYYY-MM-DD
	DataAsOf string
}

func (e *ChannelRetiredError) Error() string {
	return e.What + "不可用：" + ChannelRetiredReason + "（实际值止于 " + e.DataAsOf + "）"
}

// IsChannelRetired 判断一个错误是不是「通道已停更」，便于上层映射成明确提示而不是 500。
func IsChannelRetired(err error) bool {
	_, ok := err.(*ChannelRetiredError)
	return ok
}

// retired 构造守卫错误；DataAsOf 取运行时能确认的最晚实际值日，取不到用常量。
func retired(what string) error {
	return &ChannelRetiredError{What: what, DataAsOf: channelRetiredAsOf()}
}

// guard 默认入口的统一守卫：除非运维显式打开 gd_metric.serve_frozen，
// 否则任何「默认入口」都拿不到这个通道的数据。
//
// 开在哪里：manifest/config/config.yaml 的 gd_metric.serve_frozen，或环境变量
// GD_METRIC_SERVE_FROZEN=true。恢复步骤见 docs/data-source-matrix.md「96 点过渡态」。
func guard(ctx context.Context, what string) error {
	initGlobal(ctx)
	global.mu.RLock()
	serve := global.serveFrozen
	global.mu.RUnlock()
	if serve {
		return nil
	}
	return retired(what)
}

// channelRetiredAsOf 已加载的元数据里最晚的「有实际值」日期。
// 只读内存（不触发拉取）：守卫不该为了报一个日期去连第三方站点。
func channelRetiredAsOf() string {
	global.mu.RLock()
	days := global.days
	global.mu.RUnlock()
	if len(days) == 0 {
		return ChannelRetiredDate
	}
	if d := latestRealDate(days, 1); d != "" {
		return d
	}
	return ChannelRetiredDate
}

// 编译期提醒：ChannelRetiredDate 必须是个能解析的日期（改了字符串也要保持这个形状）。
var _ = func() bool {
	if _, err := time.ParseInLocation(dateLayout, ChannelRetiredDate, time.UTC); err != nil {
		panic("gdmetric.ChannelRetiredDate 必须是 YYYY-MM-DD: " + err.Error())
	}
	return true
}()
