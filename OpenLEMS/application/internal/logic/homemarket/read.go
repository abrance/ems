package homemarket

import (
	"context"
	"strings"
	"time"

	"s_db"
	"s_db/s_db_model"
)

// home24hPointsPerDay 一天的整点数（0..23）。缺点时均值/峰谷不给值。
const home24hPointsPerDay = 24

// Home24hPoint 一个整点。
type Home24hPoint struct {
	Hour          int     `json:"hour"`
	PowerMw       float64 `json:"powerMw"`
	AheadPrice    float64 `json:"aheadPrice"`
	RealtimePrice float64 `json:"realtimePrice"`
}

// Home24hDay 某天的 24 点分时 + 该日的聚合。
//
// Empty 显式给出：页面要区分「这天还没出（源站 T-6）」与「接口坏了」。
// 空数据不是错误，所以不用 error 表达。
type Home24hDay struct {
	Date string `json:"date"`
	// Empty 该日库里没有数据
	Empty bool `json:"empty"`
	// LatestDate 库里最近一个有数据的日期（空数据时页面提示「最新到哪天」）
	LatestDate string `json:"latestDate"`
	// LagDays 相对今天滞后几天（按 LatestDate 算；无数据时 -1）
	LagDays int            `json:"lagDays"`
	Points  []Home24hPoint `json:"points"`
	Source  string         `json:"source"`
	// PointCount 该日实际落库的点数（正常 24）。缺点的日子不能拿来当全天统计。
	PointCount int `json:"pointCount"`
	// MeanAhead / MeanRealtime 当日均值（源站也给了 todaymeanPrevPrice/todaymeanPrice，
	// 这里按落库的点自己算，保证与曲线一致）。
	//
	// 只在 24 点齐全时才给值：17 个点算出的「全天均值」会被当成结论用。缺点时
	// 这几个字段不下发（前端拿到 undefined 显示 —），并靠 PointCount 说明原因。
	MeanAhead    *float64 `json:"meanAhead,omitempty"`
	MeanRealtime *float64 `json:"meanRealtime,omitempty"`
	PeakPowerMw  *float64 `json:"peakPowerMw,omitempty"`
	LowPowerMw   *float64 `json:"lowPowerMw,omitempty"`
}

// NodePricePoint 一个地市在某时段的价格。
type NodePricePoint struct {
	City       string  `json:"city"`
	PeriodType int     `json:"periodType" dc:"1 高峰 / 2 低谷"`
	Price      float64 `json:"price"`
}

// NodePriceDay 某天的地市节点价（21 市 × 2 时段 = 42 条）。
type NodePriceDay struct {
	Date       string `json:"date"`
	Empty      bool   `json:"empty"`
	LatestDate string `json:"latestDate"`
	LagDays    int    `json:"lagDays"`
	// Points 按 periodType、city 升序
	Points []NodePricePoint `json:"points"`
	Source string           `json:"source"`
	// PeakCities / ValleyCities 各时段城市数（正常 21）
	PeakCities   int `json:"peakCities"`
	ValleyCities int `json:"valleyCities"`
}

// LoadHome24h 读某天的 24 点。date 为空时取库里最新一天。
//
// 与 spotmarket.LoadBoard 同一条约定：读路径只读已落库的数据，不在这里联网 ——
// 采集是独立任务（collect.go），读接口不该因为源站慢而变慢或变不可用。
func LoadHome24h(ctx context.Context, date string) (*Home24hDay, error) {
	svc := s_db.GetHomeMarketService()
	latest, err := svc.LatestHome24hDate(ctx)
	if err != nil {
		return nil, err
	}
	target := strings.TrimSpace(date)
	if target == "" {
		target = latest
	}
	out := &Home24hDay{Date: target, LatestDate: latest, Source: SourceHome24h, LagDays: -1}
	if latest != "" {
		out.LagDays = lagDays(latest, time.Now())
	}
	if target == "" {
		out.Empty = true
		out.Points = []Home24hPoint{}
		return out, nil
	}
	rows, err := svc.ListHome24h(ctx, target)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		out.Empty = true
		out.Points = []Home24hPoint{}
		return out, nil
	}
	points := make([]Home24hPoint, 0, len(rows))
	var sumAhead, sumRealtime float64
	peak, low := rows[0].PowerMw, rows[0].PowerMw
	for _, r := range rows {
		points = append(points, Home24hPoint{
			Hour:          r.Hour,
			PowerMw:       r.PowerMw,
			AheadPrice:    r.AheadPrice,
			RealtimePrice: r.RealtimePrice,
		})
		sumAhead += r.AheadPrice
		sumRealtime += r.RealtimePrice
		if r.PowerMw > peak {
			peak = r.PowerMw
		}
		if r.PowerMw < low {
			low = r.PowerMw
		}
		if r.Source != "" {
			out.Source = r.Source
		}
	}
	out.Points = points
	out.PointCount = len(rows)
	// 只有满 24 点才算「全天」：缺点的日子给均值/峰谷就是错数
	if out.PointCount == home24hPointsPerDay {
		meanAhead := sumAhead / float64(out.PointCount)
		meanRealtime := sumRealtime / float64(out.PointCount)
		out.MeanAhead, out.MeanRealtime = &meanAhead, &meanRealtime
		out.PeakPowerMw, out.LowPowerMw = &peak, &low
	}
	return out, nil
}

// LoadNodePrice 读某天的地市节点价。date 为空时取库里最新一天。
func LoadNodePrice(ctx context.Context, date string) (*NodePriceDay, error) {
	svc := s_db.GetHomeMarketService()
	latest, err := svc.LatestNodePriceDate(ctx)
	if err != nil {
		return nil, err
	}
	target := strings.TrimSpace(date)
	if target == "" {
		target = latest
	}
	out := &NodePriceDay{Date: target, LatestDate: latest, Source: SourceNodePrice, LagDays: -1}
	if latest != "" {
		out.LagDays = lagDays(latest, time.Now())
	}
	if target == "" {
		out.Empty = true
		out.Points = []NodePricePoint{}
		return out, nil
	}
	rows, err := svc.ListNodePrice(ctx, target)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		out.Empty = true
		out.Points = []NodePricePoint{}
		return out, nil
	}
	points := make([]NodePricePoint, 0, len(rows))
	for _, r := range rows {
		points = append(points, NodePricePoint{City: r.City, PeriodType: r.PeriodType, Price: r.Price})
		switch r.PeriodType {
		case s_db_model.NodePricePeriodPeak:
			out.PeakCities++
		case s_db_model.NodePricePeriodValley:
			out.ValleyCities++
		}
		if r.Source != "" {
			out.Source = r.Source
		}
	}
	out.Points = points
	return out, nil
}

// lagDays 数据日相对今天滞后几天。数据日比今天新（不该发生）时算 0。
func lagDays(dataDate string, now time.Time) int {
	d, err := time.ParseInLocation(dateLayout, dataDate, time.Local)
	if err != nil {
		return -1
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	lag := int(today.Sub(time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.Local)).Hours() / 24)
	if lag < 0 {
		return 0
	}
	return lag
}
