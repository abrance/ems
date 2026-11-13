package gdmetric

import (
	"context"

	"github.com/pkg/errors"
)

// 冻结通道的**只读**入口：给「分时研究（第三方 · 非官方）」页用。
//
// 与 GetFrozenSeries 只差一件事：**不联网刷新、不重写缓存元数据**。
// 「打开一页看一眼数据」不该顺手去第三方站点拉 37MB，也不该改本地缓存文件 ——
// 只读页面的副作用必须为零，否则「只是看看」就会变成一次采集。
//
// 数据仍然是那条已停更的第三方通道（实际值止于 ChannelRetiredDate），
// 所以调用方必须把「第三方 · 非官方 / 截至 X」写在页面上，且不进复盘 MAE 口径。

// ErrFrozenLocalMissing 本地没有冻结通道缓存（CI 与新机器上就是这个）。
// 单独给一个哨兵错误：页面要把「没有数据」渲染成空态，而不是 500。
var ErrFrozenLocalMissing = errors.New("本地没有第三方分时缓存（gd_metric.csv），分时研究页无数据可读")

// frozenLocalStore 只读地拿一份 store 视图：优先用已加载的元数据，缺失时直接解析 CSV。
// 返回的 store 只用于读，调用方不要往里写（curves 是新建的私有 LRU）。
func frozenLocalStore(ctx context.Context) (*store, error) {
	initGlobal(ctx)
	global.mu.RLock()
	ready := global.localCSVReady()
	path := global.cachePath
	days := global.days
	offsets := global.offsets
	maxSize := global.maxSize
	global.mu.RUnlock()
	if !ready {
		return nil, ErrFrozenLocalMissing
	}
	if len(days) == 0 || len(offsets) == 0 {
		d, off, err := ingestFile(path)
		if err != nil {
			return nil, err
		}
		days, offsets = d, off
	}
	return &store{
		cachePath: path,
		days:      days,
		offsets:   offsets,
		maxSize:   maxSize,
		curves:    newCurveLRU(maxSize),
	}, nil
}

// GetFrozenRealSeriesReadOnly 只读地取 96 点**实际值**序列（剔除 999 占位）。
//
// 只留实际值：占位行是第三方自己的预测，把它画进「第三方真值」曲线会让读者
// 分不清哪一段是观测、哪一段是推算。
func GetFrozenRealSeriesReadOnly(ctx context.Context, from, to string, kind SeriesKind) ([]TimedSample, error) {
	st, err := frozenLocalStore(ctx)
	if err != nil {
		return nil, err
	}
	all, err := st.series(from, to, kind)
	if err != nil {
		return nil, err
	}
	return realOnly(all), nil
}

// FrozenLocalDates 本地缓存里有数据的日期（升序）。from/to 为空表示不限。
func FrozenLocalDates(ctx context.Context, from, to string) ([]string, error) {
	st, err := frozenLocalStore(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(st.days))
	for _, d := range st.days {
		if from != "" && d.Date < from {
			continue
		}
		if to != "" && d.Date > to {
			continue
		}
		out = append(out, d.Date)
	}
	return out, nil
}
