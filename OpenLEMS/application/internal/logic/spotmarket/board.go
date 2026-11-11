// Package spotmarket 广东电力市场官方指标（pm.gd.csg.cn 直采，origin=pm-api）的读路径。
//
// 为什么不复用 logic/spotreport：那套是「日报 → OCR 提取 → 人工复核」的模型，
// 以 report_date 为主键、指标名带日报表号语义。官方直采没有「日报」这一层，
// 报告日与运行日同名（实时 T+2 除外），指标名是源站原名。硬塞进日报接口会让
// 两套口径互相污染（例如 report_date=D 的实时行会被当成「D 日的数据」）。
//
// 口径（与 tools/gdpm/gdpm_sqlite.py 一致，改之前先看那边）：
//   - 日前：report_date = run_date = 数据日
//   - 实时：run_date = 数据日，report_date = 数据日 + 2
//   - 电价 unit = 元/兆瓦时，电量 = MWh，户数 = 户
//   - 月频/年频的 run_date 落在期初第一天，靠 dataset 区分粒度
package spotmarket

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"s_db"
	"s_db/s_db_model"
)

const (
	// DatasetAhead 日前现货（与日报沿用同一 dataset 值，历史 OCR 行作废后不冲突）
	DatasetAhead = "day_ahead_trade"
	// DatasetRealtime 实时现货
	DatasetRealtime = "realtime_trade"

	GrainDay   = "day"
	GrainMonth = "month"
	GrainYear  = "year"

	dateLayout = "2006-01-02"

	// defaultBoardDays 日频默认回看天数（约一个季度，图上能看出峰谷与趋势）
	defaultBoardDays = 90
	maxBoardDays     = 730
	// periodicFrom 月频/年频的最早可取日期：官方表最早 2023-01
	periodicFrom = "2023-01-01"
)

// periodicGrain 月频/年频数据集 -> 粒度。日频不在这里，靠 DAILY 判断。
var periodicGrain = map[string]string{
	"mid_long_term_month": GrainMonth,
	"mid_long_term_year":  GrainYear,
	"green_power_month":   GrainMonth,
	"green_power_year":    GrainYear,
	"green_power_retail":  GrainMonth,
	"energy_retail":       GrainMonth,
	"hhi":                 GrainYear,
	"top_m":               GrainMonth,
}

// Point 一个期次上的值。
type Point struct {
	Period string  `json:"period" dc:"期次：日 2026-11-11 / 月 2026-10-01 / 年 2025-01-01"`
	Value  float64 `json:"value"`
}

// Series 一条指标序列 + 后端算好的基础统计。
//
// 环比/同比只在「相邻期次真的存在」时才给值：中间缺一天就不算环比，
// 否则拿 09-30 和 09-27 相比，会得出一个不存在的日环比。
type Series struct {
	Dataset string  `json:"dataset"`
	Scope   string  `json:"scope"`
	Metric  string  `json:"metric"`
	Unit    string  `json:"unit"`
	Grain   string  `json:"grain" dc:"day/month/year"`
	Points  []Point `json:"points" dc:"期次升序"`
	Latest  Point   `json:"latest"`
	// MomPct 环比（%），上一期不存在或不相邻时为空
	MomPct *float64 `json:"momPct"`
	// YoYPct 同比（%），去年同期没有点时为空
	YoYPct *float64 `json:"yoyPct"`
	// Peak / Valley / Spread 区间内最高、最低、峰谷价差（最高-最低）
	Peak   float64 `json:"peak"`
	Valley float64 `json:"valley"`
	Spread float64 `json:"spread"`
	Mean   float64 `json:"mean"`
	Days   int     `json:"days" dc:"参与统计的期次数"`
}

// Coverage 一个数据集的数据覆盖情况。
type Coverage struct {
	Dataset  string   `json:"dataset"`
	From     string   `json:"from"`
	To       string   `json:"to"`
	Periods  int      `json:"periods"`
	Missing  []string `json:"missing" dc:"仅日频：区间内缺的日期"`
	LastDate string   `json:"lastDate"`
}

// Board 信息披露页一次取完的数据面。
type Board struct {
	// Date 最近一个「有日前数据」的运行日；前端默认看这天
	Date string `json:"date"`
	From string `json:"from"`
	To   string `json:"to"`
	// Days 日频回看天数（实际区间以 From/To 为准）
	Days int `json:"days"`
	// Daily 日频全套：日前/实时 × 发电侧/用电侧 × 各指标
	Daily []Series `json:"daily"`
	// Periodic 月频/年频全套（中长期、绿电、零售、HHI、Top-m）
	Periodic []Series `json:"periodic"`
	// AheadRealtimeSpread 日前-实时加权均价差（元/兆瓦时，发电侧），按运行日
	AheadRealtimeSpread []Point    `json:"aheadRealtimeSpread"`
	SpreadPeak          float64    `json:"spreadPeak"`
	SpreadValley        float64    `json:"spreadValley"`
	SpreadMean          float64    `json:"spreadMean"`
	Coverage            []Coverage `json:"coverage"`
	// Source 数据来源说明，页面脚注直接展示
	Source    string `json:"source"`
	UpdatedAt string `json:"updatedAt"`
}

// LoadBoard 读 [date-days+1, date] 的日频 + 全量月频/年频，并算好基础统计。
func LoadBoard(ctx context.Context, date string, days int) (*Board, error) {
	if days <= 0 {
		days = defaultBoardDays
	}
	if days > maxBoardDays {
		days = maxBoardDays
	}
	to := strings.TrimSpace(date)
	if to == "" {
		to = time.Now().Format(dateLayout)
	}
	from, err := shiftDays(to, -days+1)
	if err != nil {
		return nil, err
	}

	// 日频多取一年：同比要去年同期那个点，只按展示窗口取数就永远算不出同比
	// （2026-11 的窗口里没有 2025-11，同比恒为空）。
	lookupFrom, err := shiftDays(from, -366)
	if err != nil {
		return nil, err
	}

	svc := s_db.GetSpotExtractService()
	dailyRows, err := svc.ListMetricsByRunDateRange(ctx, lookupFrom, to)
	if err != nil {
		return nil, err
	}
	periodicRows, err := svc.ListMetricsByRunDateRange(ctx, periodicFrom, to)
	if err != nil {
		return nil, err
	}

	daily := groupSeries(dailyRows, func(dataset string) bool { return periodicGrain[dataset] == "" }, from)
	periodic := groupSeries(periodicRows, func(dataset string) bool { return periodicGrain[dataset] != "" }, "")

	board := &Board{
		From:     from,
		To:       to,
		Days:     days,
		Daily:    daily,
		Periodic: periodic,
		Source:   "广东电力交易中心 信息披露接口（pm.gd.csg.cn）直采，origin=pm-api",
	}
	board.Date = latestDate(dailyRows)
	if board.Date == "" {
		board.Date = to
	}
	board.AheadRealtimeSpread, board.SpreadPeak, board.SpreadValley, board.SpreadMean =
		aheadRealtimeSpread(daily)
	board.Coverage = coverage(dailyRows, periodicRows, from, to)
	if len(dailyRows) > 0 {
		board.UpdatedAt = to
	}
	return board, nil
}

// groupSeries 把明细行按 (dataset, scope, metric) 收成序列，并算统计。
//
// windowFrom 非空时只保留该日期之后的点（日频只要展示窗口），但同比仍用窗口外的历史算。
func groupSeries(rows []*s_db_model.SSpotMetricModel, pick func(dataset string) bool, windowFrom string) []Series {
	idx := map[[3]string]*Series{}
	points := map[*Series][]Point{}
	for _, r := range rows {
		if r == nil || !pick(r.Dataset) {
			continue
		}
		key := [3]string{r.Dataset, r.Scope, r.Metric}
		s := idx[key]
		if s == nil {
			s = &Series{Dataset: r.Dataset, Scope: r.Scope, Metric: r.Metric, Unit: r.Unit,
				Grain: grainOf(r.Dataset)}
			idx[key] = s
		}
		if s.Unit == "" {
			s.Unit = r.Unit
		}
		points[s] = append(points[s], Point{Period: r.RunDate, Value: r.Value})
	}
	out := make([]Series, 0, len(idx))
	for _, s := range idx {
		pts := points[s]
		sort.Slice(pts, func(i, j int) bool { return pts[i].Period < pts[j].Period })
		s.Points = pts
		applyStats(s, windowFrom)
		if windowFrom != "" && len(s.Points) == 0 {
			continue
		}
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dataset != out[j].Dataset {
			return out[i].Dataset < out[j].Dataset
		}
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		return out[i].Metric < out[j].Metric
	})
	return out
}

// applyStats 填 latest / 环比 / 同比 / 峰谷。
//
// 顺序要紧：先用**全量**点算同比（去年同期可能在窗口外），再把点裁到窗口内算
// 环比与峰谷 —— 否则环比会拿窗口边界上的两个点当相邻期次。
func applyStats(s *Series, windowFrom string) {
	if len(s.Points) == 0 {
		return
	}
	allByPeriod := make(map[string]float64, len(s.Points))
	for _, p := range s.Points {
		if _, ok := allByPeriod[p.Period]; !ok {
			allByPeriod[p.Period] = p.Value
		}
	}
	if windowFrom != "" {
		trimmed := s.Points[:0]
		for _, p := range s.Points {
			if p.Period >= windowFrom {
				trimmed = append(trimmed, p)
			}
		}
		s.Points = trimmed
		if len(s.Points) == 0 {
			return
		}
	}

	last := s.Points[len(s.Points)-1]
	s.Latest = last
	s.Days = len(s.Points)
	s.Peak, s.Valley = s.Points[0].Value, s.Points[0].Value
	sum := 0.0
	byPeriod := make(map[string]float64, len(s.Points))
	for _, p := range s.Points {
		if p.Value > s.Peak {
			s.Peak = p.Value
		}
		if p.Value < s.Valley {
			s.Valley = p.Value
		}
		sum += p.Value
		byPeriod[p.Period] = p.Value
	}
	s.Mean = sum / float64(len(s.Points))
	s.Spread = s.Peak - s.Valley

	if prev, ok := byPeriod[stepBack(last.Period, s.Grain, 1)]; ok && prev != 0 {
		s.MomPct = pct(last.Value, prev)
	}
	if yoy, ok := allByPeriod[samePeriodLastYear(last.Period, s.Grain)]; ok && yoy != 0 {
		s.YoYPct = pct(last.Value, yoy)
	}
}

// samePeriodLastYear 去年同期：日按整年（不是 365 天，闰年会偏一天），月按 -12 个月。
func samePeriodLastYear(period, grain string) string {
	t, err := time.ParseInLocation(dateLayout, period, time.Local)
	if err != nil {
		return ""
	}
	if grain == GrainMonth {
		return t.AddDate(0, -12, 0).Format(dateLayout)
	}
	return t.AddDate(-1, 0, 0).Format(dateLayout)
}

// stepBack 按粒度回退 n 期；解析失败返回空串。
func stepBack(period, grain string, n int) string {
	t, err := time.ParseInLocation(dateLayout, period, time.Local)
	if err != nil {
		return ""
	}
	switch grain {
	case GrainYear:
		return t.AddDate(-n, 0, 0).Format(dateLayout)
	case GrainMonth:
		return t.AddDate(0, -n, 0).Format(dateLayout)
	default:
		return t.AddDate(0, 0, -n).Format(dateLayout)
	}
}

func pct(now, before float64) *float64 {
	v := (now - before) / before * 100
	return &v
}

func grainOf(dataset string) string {
	if g, ok := periodicGrain[dataset]; ok {
		return g
	}
	return GrainDay
}

// latestDate 最近一个日前运行日：日前当日发布，比实时（T+2）可靠。
func latestDate(rows []*s_db_model.SSpotMetricModel) string {
	latest := ""
	for _, r := range rows {
		if r == nil || r.Dataset != DatasetAhead {
			continue
		}
		if r.RunDate > latest {
			latest = r.RunDate
		}
	}
	return latest
}

// aheadRealtimeSpread 同一运行日的「日前加权均价 - 实时加权均价」（发电侧）。
//
// 实时只披露发电侧加权均价，所以这条差价也只有发电侧一个口径。
func aheadRealtimeSpread(daily []Series) (pts []Point, peak, valley, mean float64) {
	ahead := map[string]float64{}
	for _, s := range daily {
		if s.Dataset == DatasetAhead && strings.Contains(s.Metric, "加权平均电价") {
			for _, p := range s.Points {
				ahead[p.Period] = p.Value
			}
		}
	}
	dates := make([]string, 0, len(ahead))
	for d := range ahead {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	real := map[string]float64{}
	for _, s := range daily {
		if s.Dataset == DatasetRealtime && strings.Contains(s.Metric, "加权平均电价") {
			for _, p := range s.Points {
				real[p.Period] = p.Value
			}
		}
	}
	sum := 0.0
	for _, d := range dates {
		r, ok := real[d]
		if !ok {
			continue
		}
		v := ahead[d] - r
		pts = append(pts, Point{Period: d, Value: round2(v)})
		sum += v
	}
	if len(pts) > 0 {
		peak, valley = pts[0].Value, pts[0].Value
		for _, p := range pts {
			if p.Value > peak {
				peak = p.Value
			}
			if p.Value < valley {
				valley = p.Value
			}
		}
		mean = round2(sum / float64(len(pts)))
	}
	return pts, round2(peak), round2(valley), mean
}

// coverage 每个数据集的首末日期与缺日（只对日频追缺日）。
func coverage(dailyRows, periodicRows []*s_db_model.SSpotMetricModel, from, to string) []Coverage {
	type acc struct {
		days map[string]bool
	}
	m := map[string]*acc{}
	for _, rows := range [][]*s_db_model.SSpotMetricModel{dailyRows, periodicRows} {
		for _, r := range rows {
			if r == nil {
				continue
			}
			// 日频多取的那一年只用于算同比，不该算进区间覆盖
			if periodicGrain[r.Dataset] == "" && r.RunDate < from {
				continue
			}
			a := m[r.Dataset]
			if a == nil {
				a = &acc{days: map[string]bool{}}
				m[r.Dataset] = a
			}
			a.days[r.RunDate] = true
		}
	}
	out := make([]Coverage, 0, len(m))
	for dataset, a := range m {
		dates := make([]string, 0, len(a.days))
		for d := range a.days {
			dates = append(dates, d)
		}
		sort.Strings(dates)
		c := Coverage{Dataset: dataset, From: dates[0], To: dates[len(dates)-1],
			Periods: len(dates), LastDate: dates[len(dates)-1]}
		if periodicGrain[dataset] == "" && dataset != "" {
			c.Missing = missingDays(a.days, from, to)
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dataset < out[j].Dataset })
	return out
}

// missingDays 区间内缺哪几天。只看区间本身，不看数据首末之外的部分。
func missingDays(have map[string]bool, from, to string) []string {
	start, err1 := time.ParseInLocation(dateLayout, from, time.Local)
	end, err2 := time.ParseInLocation(dateLayout, to, time.Local)
	if err1 != nil || err2 != nil || start.After(end) {
		return nil
	}
	out := make([]string, 0)
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 1) {
		d := cur.Format(dateLayout)
		if !have[d] {
			out = append(out, d)
		}
	}
	return out
}

func shiftDays(day string, n int) (string, error) {
	t, err := time.ParseInLocation(dateLayout, day, time.Local)
	if err != nil {
		return "", err
	}
	return t.AddDate(0, 0, n).Format(dateLayout), nil
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
