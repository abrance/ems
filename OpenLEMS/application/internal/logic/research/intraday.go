package research

import (
	"context"
	"errors"
	"time"

	"application/internal/logic/gdmetric"
)

// 分时研究（第三方 · 非官方）的取数口径。
//
// 这一层只做「把冻结快照装配成一个响应」，不做 HTTP、不做采集：
// 页面要的三件事都在这里定死 ——
//   - asOf = 最后一天有实际值（不是今天），页面写「截至 X」；
//   - 区间内没入库的日期进 Missing，页面渲染空态，**不补 0、不连直线**；
//   - 本地完全没数据时返回空 Days（CI / 新机器就是这样），不是错误。
//
// 单位一律 元/兆瓦时（源站口径），不做换算：这一页不参与复盘 MAE，
// 唯一的要求是「读者知道这是第三方观测值」。

const (
	// DefaultWindowDays 默认窗口：截至日往前 7 天（含截至日）。
	DefaultWindowDays = 7
	// StaleAfterDays 与官方日频同阈值：>7 天算过期，恰好 7 天仍是滞后。
	StaleAfterDays = 7

	// 源口径写死在代码里：一个响应只能有一个 source，不许前端拼。
	SourceID   = "gdmetric.frozen"
	SourceName = "第三方分时快照（gd_metric.csv，已停更）"
	SourceKind = "thirdparty"
)

// Source 数据源元信息。
type Source struct {
	ID   string
	Name string
	Kind string
}

// Point 一个 15 分钟点。
type Point struct {
	Time  string
	Value float64
}

// Day 某运行日的 96 点。
type Day struct {
	Date   string
	Points []Point
}

// Result 装配结果（不含 HTTP 形态）。
type Result struct {
	Source    Source
	AsOf      string
	LagDays   int
	Stale     bool
	From      string
	To        string
	Days      []Day
	Missing   []string
	Available []string
}

// Load 读冻结快照并装配响应。
//
// from/to 为空时：to 锚到 asOf（不是今天 —— 否则「近 7 天」整段落在数据之外，
// 打开页面一片空态，复盘页踩过这个坑），from = to 往前 6 天。
func Load(ctx context.Context, from, to string, now time.Time) (*Result, error) {
	res := &Result{
		Source:    Source{ID: SourceID, Name: SourceName, Kind: SourceKind},
		LagDays:   -1,
		Days:      []Day{},
		Missing:   []string{},
		Available: []string{},
	}

	available, err := gdmetric.FrozenLocalDates(ctx, "", "")
	if err != nil && !isMissing(err) {
		return nil, err
	}
	res.Available = available
	if len(available) > 0 {
		res.AsOf = available[len(available)-1]
		res.LagDays = LagDays(res.AsOf, now)
		res.Stale = res.LagDays > StaleAfterDays
	}

	if to == "" {
		to = res.AsOf
	}
	if to == "" {
		// 完全没有数据：把请求的起点原样回给前端，页面据此说「未入库」。
		res.From = from
		res.Missing = DateSpan(from, "")
		return res, nil
	}
	if from == "" {
		from = ShiftDays(to, -(DefaultWindowDays - 1))
	}
	res.From, res.To = from, to

	span := DateSpan(from, to)
	if res.AsOf == "" {
		res.Missing = span
		return res, nil
	}

	points, err := gdmetric.GetFrozenRealSeriesReadOnly(ctx, from, to, gdmetric.KindDayAhead)
	if err != nil && !isMissing(err) {
		return nil, err
	}
	byDate := map[string][]Point{}
	for _, p := range points {
		byDate[p.Date] = append(byDate[p.Date], Point{Time: p.Time, Value: p.Value})
	}
	for _, d := range span {
		pts, ok := byDate[d]
		if !ok || len(pts) == 0 {
			res.Missing = append(res.Missing, d)
			continue
		}
		res.Days = append(res.Days, Day{Date: d, Points: pts})
	}
	return res, nil
}

// isMissing 用 errors.Is：错误一旦被 Wrap（空态那条路径以后加个上下文就会），
// == 立刻失效，而失效的方向是「当成真错误返回 500」—— 页面从空态变成报错。
func isMissing(err error) bool {
	return errors.Is(err, gdmetric.ErrFrozenLocalMissing)
}

// LagDays 数据日到 now 的自然日数（按 Asia/Shanghai 的日历日）；格式不对返回 -1。
//
// 固定时区：同一份数据在 UTC 容器里的无头浏览器不能差一天。
func LagDays(date string, now time.Time) int {
	d, err := time.ParseInLocation("2006-01-02", date, shanghai())
	if err != nil {
		return -1
	}
	n := now.In(shanghai())
	today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, shanghai())
	return int(today.Sub(time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, shanghai())).Hours() / 24)
}

// ShiftDays 日期加减天数；格式不对原样返回。
func ShiftDays(date string, n int) string {
	t, err := time.ParseInLocation("2006-01-02", date, time.UTC)
	if err != nil {
		return date
	}
	return t.AddDate(0, 0, n).Format("2006-01-02")
}

// DateSpan [from, to] 的每一天（含两端）。任一端非法或 to<from 时返回空。
func DateSpan(from, to string) []string {
	start, err1 := time.ParseInLocation("2006-01-02", from, time.UTC)
	end, err2 := time.ParseInLocation("2006-01-02", to, time.UTC)
	if err1 != nil || err2 != nil || end.Before(start) {
		return nil
	}
	out := make([]string, 0, int(end.Sub(start).Hours()/24)+1)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format("2006-01-02"))
	}
	return out
}

func shanghai() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}
