package gdmetric

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// 区间序列读取：给预测/复盘用。
//
// 为什么按天读而不是全表扫：CSV 有 50 万行、37MB，而 store 已经为每个日期记了
// 文件偏移（offsets），按天读是 O(该天行数)，还带 LRU。只有日期不存在时才退化。

// SeriesKind 指标类别（与 snapshot 的 load/dayAhead/realtime 对应）
type SeriesKind string

const (
	KindDayAhead SeriesKind = "dayAhead"
	KindRealtime SeriesKind = "realtime"
	KindLoad     SeriesKind = "load"
)

// TimedSample 带日期时间的样本。Time 是 "HH:MM"，Date 是 "YYYY-MM-DD"。
type TimedSample struct {
	Date  string  `json:"date"`
	Time  string  `json:"time"`
	Value float64 `json:"value"`
	// SourceID 见 gdmetric.SourceExchange / SourceForecastPlaceholder：
	// 999 表示这一时刻的**实际值还没出**，值是电查查的预测占位。
	SourceID int `json:"sourceId"`
}

// NormalizeKind 归一化类别名，接受常见别名。
func NormalizeKind(kind string) (SeriesKind, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "dayahead", "day_ahead", "day-ahead", "ahead":
		return KindDayAhead, nil
	case "realtime", "real", "rt":
		return KindRealtime, nil
	case "load":
		return KindLoad, nil
	default:
		return "", errors.Errorf("不支持的指标类别: %s（可用 dayAhead / realtime / load）", kind)
	}
}

// MaxSeriesDays 单次区间读取的硬上限。
//
// 不是为了省内存，而是为了让调用方早失败：429 天 × 96 点 ≈ 4.1 万点，
// 已经超过 TimesFM 的 max_context(4096)；再长只会算得更慢而结果不会更好。
const MaxSeriesDays = 400

// GetSeries 返回 [from, to] 内某类指标的 96 点序列，按时间升序。
// 区间内没有数据的日期直接跳过（不补洞）。
func GetSeries(ctx context.Context, from, to string, kind SeriesKind) ([]TimedSample, error) {
	initGlobal(ctx)
	if err := global.ensureFresh(ctx); err != nil {
		global.mu.RLock()
		empty := len(global.days) == 0
		global.mu.RUnlock()
		if empty {
			return nil, err
		}
		// 有缓存就沿用：复盘看的是历史，不该因为一次拉取失败而打不开
	}
	return global.series(from, to, kind)
}

// series 读区间序列（不含初始化/刷新）。拆出来是为了能在测试里直接跑，不去碰网络。
func (s *store) series(from, to string, kind SeriesKind) ([]TimedSample, error) {
	loc := time.UTC
	fromT, err := time.ParseInLocation(dateLayout, strings.TrimSpace(from), loc)
	if err != nil {
		return nil, errors.Errorf("起始日期无效: %s", from)
	}
	toT, err := time.ParseInLocation(dateLayout, strings.TrimSpace(to), loc)
	if err != nil {
		return nil, errors.Errorf("结束日期无效: %s", to)
	}
	if toT.Before(fromT) {
		return nil, errors.New("结束日期不能早于起始日期")
	}
	if days := int(toT.Sub(fromT).Hours()/24) + 1; days > MaxSeriesDays {
		return nil, errors.Errorf("区间过长（%d 天），上限 %d 天", days, MaxSeriesDays)
	}

	// 只遍历 store 里真实存在的日期，避免对每个空日期都做一次文件读
	s.mu.RLock()
	exist := make(map[string]bool, len(s.days))
	for _, d := range s.days {
		exist[d.Date] = true
	}
	s.mu.RUnlock()

	out := make([]TimedSample, 0, int(toT.Sub(fromT).Hours()/24+1)*96)
	for d := fromT; !d.After(toT); d = d.AddDate(0, 0, 1) {
		date := d.Format(dateLayout)
		if !exist[date] {
			continue
		}
		curve, err := s.getCurve(date)
		if err != nil {
			return nil, errors.Wrapf(err, "读取 %s 曲线失败", date)
		}
		if curve == nil {
			continue
		}
		var samples []Sample
		switch kind {
		case KindDayAhead:
			samples = curve.DayAhead
		case KindRealtime:
			samples = curve.Realtime
		case KindLoad:
			samples = curve.Load
		}
		for _, smp := range samples {
			out = append(out, TimedSample{Date: date, Time: smp.Time, Value: smp.Value, SourceID: smp.SourceID})
		}
	}
	return out, nil
}

// GetRealSeries 同 GetSeries，但只保留**实际值**（剔除电查查预测占位 999）。
//
// 复盘/回测必须用它：占位行是另一个模型的预测，拿它当基准真值去算准确度，
// 算出来的只是「两个模型差多少」，与「预测准不准」无关。
func GetRealSeries(ctx context.Context, from, to string, kind SeriesKind) ([]TimedSample, error) {
	all, err := GetSeries(ctx, from, to, kind)
	if err != nil {
		return nil, err
	}
	return realOnly(all), nil
}

// realOnly 剔除占位来源（999）与未知来源（0），只留实际值。
func realOnly(all []TimedSample) []TimedSample {
	out := make([]TimedSample, 0, len(all))
	for _, s := range all {
		if !IsRealSource(s.SourceID) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// AvailableDates 返回区间内确实有数据的日期（升序），供前端灰显无数据的日期。
func AvailableDates(ctx context.Context, from, to string) ([]string, error) {
	series, err := GetSeries(ctx, from, to, KindDayAhead)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, s := range series {
		set[s.Date] = true
	}
	dates := make([]string, 0, len(set))
	for d := range set {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	return dates, nil
}
