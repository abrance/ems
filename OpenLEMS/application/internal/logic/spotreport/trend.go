package spotreport

import (
	"fmt"
	"sort"
	"strings"
	"time"

	v1 "application/api/spotreport/v1"
	"s_db/s_db_model"
)

// 趋势聚合：把按运行日落库的指标分桶成 日/周/月 序列，供首页「电力现货趋势」卡片画图。
//
// 口径（业务逐条确认，改动前先看这里）：
//  1. 时间轴用 run_date（运行日），不是报告发布日。同一运行日可能由多份日报产出：
//     表1(二) 是当日报告的口径，表2(一) 是滞后 T-2 的复核值（少新能源、也没有环比）。
//     日前成交只取 表1(二)，否则同一天会出现两个点。实时成交只有 表2(二) 一路，照用。
//  2. 只看已确认（origin=manual）。未复核整行剔除，桶里没有点就不画，不做补洞。
//  3. 每桶每序列输出 avg/max/min 与参与天数；同键同一天的重复行只算一次。
//  4. 申报容量结构不用平均 share_pct，而是桶内 Σ容量 ÷ Σ申报容量 重算，避免小样本天数拉偏。
//  5. 地市档位（node_price_*）是分类值不是价格：输出「各档位城市-天计数」。

// TrendGrain 趋势分桶粒度
type TrendGrain string

const (
	GrainDay   TrendGrain = "day"
	GrainWeek  TrendGrain = "week"
	GrainMonth TrendGrain = "month"
)

const (
	// TrendMaxRangeDays 区间上限：再长就该换粒度了，别把全表搬进内存
	TrendMaxRangeDays = 730
	// trendAutoDayMaxSpan / trendAutoWeekMaxSpan 粒度自动推断的跨度阈值（天）
	trendAutoDayMaxSpan  = 62
	trendAutoWeekMaxSpan = 400
	trendDateLayout      = "2006-01-02"
	trendGroupSeparator  = "\x00"
)

// trendDatasetOrder 输出顺序：与数据页的数据集顺序一致，前端按此顺序排图
var trendDatasetOrder = map[string]int{
	DatasetSummary:      0,
	DatasetTrade:        1,
	DatasetRealtime:     2,
	DatasetDecl:         3,
	"node_price_peak":   4,
	"node_price_valley": 5,
}

// TrendPoint 一个桶内一条序列的聚合结果（前端 6 张图共用这一种形状）
type TrendPoint struct {
	Bucket  string  `json:"bucket" dc:"桶键：2026-09-20 / 2026-W39 / 2026-09"`
	From    string  `json:"from" dc:"桶内实际最早运行日"`
	To      string  `json:"to" dc:"桶内实际最晚运行日"`
	Dataset string  `json:"dataset"`
	Scope   string  `json:"scope"`
	Metric  string  `json:"metric"`
	Avg     float64 `json:"avg"`
	Max     float64 `json:"max"`
	Min     float64 `json:"min"`
	Sum     float64 `json:"sum" dc:"桶内求和（容量类用于占比重算与提示）"`
	Days    int     `json:"days" dc:"桶内参与统计的运行日数"`
	Unit    string  `json:"unit"`
	// SharePct 占比：仅申报容量类指标有值，= Σ容量 ÷ Σ申报容量 × 100（桶内重算）
	SharePct *float64 `json:"sharePct,omitempty"`
	// Value / TextValue / Count 仅地市档位用：档位序号、区间文本、命中该档位的城市-天数
	Value     float64 `json:"value"`
	TextValue string  `json:"textValue,omitempty"`
	Count     int     `json:"count,omitempty"`
}

// NormalizeTrendRange 兜底并校验趋势区间：缺省与非法值落到「本月 1 日 → 今天」，
// 起止颠倒直接报错（静默对调会让人以为查的是另一段时间），跨度上限 TrendMaxRangeDays。
func NormalizeTrendRange(from, to string) (string, string, error) {
	loc := shanghaiLocation()
	today := time.Now().In(loc).Format(trendDateLayout)
	fromT, err := time.ParseInLocation(trendDateLayout, strings.TrimSpace(from), loc)
	if err != nil {
		from = today[:8] + "01"
		fromT, _ = time.ParseInLocation(trendDateLayout, from, loc)
	}
	toT, err := time.ParseInLocation(trendDateLayout, strings.TrimSpace(to), loc)
	if err != nil {
		to = today
		toT, _ = time.ParseInLocation(trendDateLayout, to, loc)
	}
	if toT.Before(fromT) {
		return "", "", fmt.Errorf("结束日期不能早于起始日期")
	}
	if span := int(toT.Sub(fromT).Hours()/24) + 1; span > TrendMaxRangeDays {
		return "", "", fmt.Errorf("时间范围最多 %d 天（当前 %d 天）", TrendMaxRangeDays, span)
	}
	return from, to, nil
}

// DefaultTrendGrain 按区间跨度推断默认粒度：≤62 天按日、≤400 天按周、更长按月
func DefaultTrendGrain(from, to string) TrendGrain {
	loc := shanghaiLocation()
	fromT, err1 := time.ParseInLocation(trendDateLayout, from, loc)
	toT, err2 := time.ParseInLocation(trendDateLayout, to, loc)
	if err1 != nil || err2 != nil {
		return GrainDay
	}
	span := int(toT.Sub(fromT).Hours()/24) + 1
	switch {
	case span <= trendAutoDayMaxSpan:
		return GrainDay
	case span <= trendAutoWeekMaxSpan:
		return GrainWeek
	default:
		return GrainMonth
	}
}

// ParseTrendGrain 解析显式粒度，非法值返回 false（由调用方回落到 DefaultTrendGrain）
func ParseTrendGrain(grain string) (TrendGrain, bool) {
	switch TrendGrain(strings.TrimSpace(grain)) {
	case GrainDay:
		return GrainDay, true
	case GrainWeek:
		return GrainWeek, true
	case GrainMonth:
		return GrainMonth, true
	default:
		return "", false
	}
}

// BucketOf 把运行日映射到桶键。周用 ISO 周（周一为一周之始，与「本周」快捷区间的口径一致）。
func BucketOf(grain TrendGrain, runDate string) (string, bool) {
	t, err := time.ParseInLocation(trendDateLayout, runDate, shanghaiLocation())
	if err != nil {
		return "", false
	}
	switch grain {
	case GrainWeek:
		year, week := t.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", year, week), true
	case GrainMonth:
		return t.Format("2006-01"), true
	default:
		return runDate, true
	}
}

// IsBucketDataset 档位类数据集：值是分类档位而非价格，聚合时走计数分支
func IsBucketDataset(dataset string) bool {
	return dataset == "node_price_peak" || dataset == "node_price_valley"
}

func isCapacityMetric(metric string) bool {
	switch metric {
	case MetricDeclVolume, MetricCoalCap, MetricGasCap, MetricNewCap:
		return true
	}
	return false
}

type trendAcc struct {
	bucket    string
	dataset   string
	scope     string
	metric    string
	unit      string
	textValue string
	value     float64
	from      string
	to        string
	sum       float64
	min       float64
	max       float64
	first     bool
	count     int
	dates     map[string]bool
}

// BuildTrend 聚合趋势点。入参是已按运行日区间取出的明细（可含未复核行，本函数自己过滤：
// 口径与单测都留在一处，s_db 侧只是顺手把未复核行挡在 SQL 里）。
func BuildTrend(models []*s_db_model.SSpotMetricModel, grain TrendGrain) []TrendPoint {
	groups := map[string]*trendAcc{}
	declTotal := map[string]float64{} // bucket+scope → Σ申报容量
	for _, m := range models {
		if !IsConfirmed(m.Origin) {
			continue
		}
		if m.Dataset == DatasetTrade && m.Source == SourceTradeLag {
			continue // 滞后复核值，与当日的表1(二) 是同一运行日，取一路即可
		}
		bucket, ok := BucketOf(grain, m.RunDate)
		if !ok {
			continue
		}
		// 档位类数据集的「城市」是样本不是维度：按档位区间聚合，否则每个城市各成一条序列
		seriesKey := m.Scope
		if IsBucketDataset(m.Dataset) {
			seriesKey = m.TextValue
		}
		key := strings.Join([]string{bucket, m.Dataset, seriesKey, m.Metric}, trendGroupSeparator)
		acc := groups[key]
		if acc == nil {
			acc = &trendAcc{bucket: bucket, dataset: m.Dataset, scope: m.Scope, metric: m.Metric,
				unit: m.Unit, from: m.RunDate, to: m.RunDate, dates: map[string]bool{}}
			groups[key] = acc
		}
		if acc.unit == "" {
			acc.unit = m.Unit
		}
		if m.RunDate < acc.from {
			acc.from = m.RunDate
		}
		if m.RunDate > acc.to {
			acc.to = m.RunDate
		}
		if !IsBucketDataset(m.Dataset) && acc.dates[m.RunDate] {
			continue // 数值序列同一运行日的重复行只算一次
		}
		acc.dates[m.RunDate] = true
		acc.count++
		if IsBucketDataset(m.Dataset) {
			// 档位计数：每个城市-天都是一个样本，不做同日去重
			acc.textValue = m.TextValue
			acc.value = m.Value
			continue
		}
		acc.sum += m.Value
		if !acc.first {
			acc.min, acc.max, acc.first = m.Value, m.Value, true
		} else {
			if m.Value < acc.min {
				acc.min = m.Value
			}
			if m.Value > acc.max {
				acc.max = m.Value
			}
		}
		if m.Dataset == DatasetDecl && m.Metric == MetricDeclVolume {
			declTotal[bucket+trendGroupSeparator+m.Scope] += m.Value
		}
	}

	points := make([]TrendPoint, 0, len(groups))
	for _, acc := range groups {
		point := TrendPoint{
			Bucket: acc.bucket, From: acc.from, To: acc.to,
			Dataset: acc.dataset, Scope: acc.scope, Metric: acc.metric,
			Sum: acc.sum, Days: len(acc.dates), Unit: acc.unit,
			Value: acc.value, TextValue: acc.textValue, Count: acc.count,
		}
		if IsBucketDataset(acc.dataset) {
			// 档位点没有 scope 维度（城市已计入 Count）
			point.Scope = ""
		} else {
			point.Avg = acc.sum / float64(len(acc.dates))
			point.Max, point.Min = acc.max, acc.min
		}
		if acc.dataset == DatasetDecl && isCapacityMetric(acc.metric) {
			if total := declTotal[acc.bucket+trendGroupSeparator+acc.scope]; total > 0 {
				share := acc.sum / total * 100
				point.SharePct = &share
			}
		}
		points = append(points, point)
	}
	sort.SliceStable(points, func(i, j int) bool {
		return trendLess(points[i], points[j])
	})
	return points
}

func trendLess(a, b TrendPoint) bool {
	if a.Bucket != b.Bucket {
		return a.Bucket < b.Bucket
	}
	if trendDatasetOrder[a.Dataset] != trendDatasetOrder[b.Dataset] {
		return trendDatasetOrder[a.Dataset] < trendDatasetOrder[b.Dataset]
	}
	if a.Scope != b.Scope {
		return a.Scope < b.Scope
	}
	if a.Metric != b.Metric {
		return a.Metric < b.Metric
	}
	// 档位点的 scope/metric 相同，用档位序号收尾，否则顺序随 map 遍历漂移
	if a.Value != b.Value {
		return a.Value < b.Value
	}
	return a.TextValue < b.TextValue
}

// ToTrendPoints 转成接口出参（api 包不能反向依赖 logic，映射只能放这边）
func ToTrendPoints(points []TrendPoint) []*v1.SpotTrendPoint {
	out := make([]*v1.SpotTrendPoint, 0, len(points))
	for _, p := range points {
		out = append(out, &v1.SpotTrendPoint{
			Bucket: p.Bucket, From: p.From, To: p.To,
			Dataset: p.Dataset, Scope: p.Scope, Metric: p.Metric,
			Avg: p.Avg, Max: p.Max, Min: p.Min, Sum: p.Sum, Days: p.Days, Unit: p.Unit,
			SharePct: p.SharePct, Value: p.Value, TextValue: p.TextValue, Count: p.Count,
		})
	}
	return out
}
