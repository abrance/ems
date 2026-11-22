package forecast

import (
	"context"
	"strings"
	"time"

	v1 "application/api/forecast/v1"
	forecastlogic "application/internal/logic/forecast"

	"s_db"
	"s_db/s_db_model"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// GetAlgoReview 小时电价预测算法回测：只读留痕，不重跑模型。
//
// 预测侧：gd_price_forecast 里 grain=hour 的当时留痕，按 algo 分组；
// 实际侧：gd_home_24h 的 ahead_price / realtime_price；
// 配对键：(目标日, 整点)。融合由逻辑层实时算，不落库。
func (c *ControllerV1) GetAlgoReview(ctx context.Context, req *v1.GetAlgoReviewReq) (res *v1.GetAlgoReviewRes, err error) {
	market := strings.ToLower(strings.TrimSpace(req.Market))
	if market == "" {
		market = "ahead"
	}
	if market != "ahead" && market != "real" {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "market 只能是 ahead 或 real")
	}

	from := strings.TrimSpace(req.From)
	to := strings.TrimSpace(req.To)
	start, err := time.Parse("2006-01-02", from)
	if err != nil {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "from 必须是 YYYY-MM-DD")
	}
	end, err := time.Parse("2006-01-02", to)
	if err != nil {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "to 必须是 YYYY-MM-DD")
	}
	if end.Before(start) {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "to 不能早于 from")
	}
	if end.Sub(start) > 366*24*time.Hour {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "回测区间不能超过 366 天")
	}

	algos, err := forecastlogic.ParseAlgos(req.Algos)
	if err != nil {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, err.Error())
	}

	detailDate := strings.TrimSpace(req.Date)
	if detailDate != "" {
		if _, perr := time.Parse("2006-01-02", detailDate); perr != nil {
			return nil, gerror.NewCode(gcode.CodeInvalidParameter, "date 必须是 YYYY-MM-DD")
		}
		if detailDate < from || detailDate > to {
			return nil, gerror.NewCode(gcode.CodeInvalidParameter, "date 必须落在 from ~ to 之间")
		}
	}

	// target_time 存到分钟，to 必须带到当天末尾，否则最后一天会被字典序排除。
	rows, err := s_db.GetPriceForecastService().ListByTargetRange(
		ctx, from, to+" 23:59", market, s_db_model.ForecastGrainHour)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "读取预测留痕失败")
	}

	actualRows, err := s_db.GetHomeMarketService().ListHome24hRange(ctx, from, to)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "读取官网 24 点实际值失败")
	}
	actual := make(map[string]map[int]float64, len(actualRows))
	for _, r := range actualRows {
		price := r.AheadPrice
		if market == "real" {
			price = r.RealtimePrice
		}
		if actual[r.PriceDate] == nil {
			actual[r.PriceDate] = make(map[int]float64, 24)
		}
		actual[r.PriceDate][r.Hour] = price
	}

	unit := ""
	for _, r := range rows {
		if strings.TrimSpace(r.Unit) != "" {
			unit = strings.TrimSpace(r.Unit)
			break
		}
	}
	if unit == "" {
		unit = "元/兆瓦时"
	}

	out := forecastlogic.BuildAlgoReview(rows, actual, from, to, unit, algos, detailDate)

	dataAsOf := ""
	lagDays := -1
	if latest, lerr := s_db.GetHomeMarketService().LatestHome24hDate(ctx); lerr == nil {
		dataAsOf = strings.TrimSpace(latest)
	}
	if dataAsOf != "" {
		loc := time.FixedZone("CST", 8*3600)
		if t, perr := time.ParseInLocation("2006-01-02", dataAsOf, loc); perr == nil {
			now := time.Now().In(loc)
			today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
			lagDays = int(today.Sub(t).Hours() / 24)
			if lagDays < 0 {
				lagDays = 0
			}
		}
	}

	res = &v1.GetAlgoReviewRes{
		Market:      market,
		Unit:        out.Unit,
		From:        out.From,
		To:          out.To,
		Algos:       out.Algos,
		Summary:     make([]*v1.AlgoReviewSummary, 0, len(out.Summary)),
		Daily:       make([]*v1.AlgoReviewDaily, 0, len(out.Daily)),
		Hourly:      make([]*v1.AlgoReviewHourly, 0, len(out.Hourly)),
		Detail:      make([]*v1.AlgoReviewPoint, 0, len(out.Detail)),
		Dropped:     &v1.AlgoReviewDropped{},
		DataAsOf:    dataAsOf,
		LagDays:     lagDays,
		TruthSource: "官网主页 24 点分时（gd_home_24h）",
	}
	if out.Dropped != nil {
		res.Dropped.NoActual = out.Dropped.NoActual
		res.Dropped.Backfilled = out.Dropped.Backfilled
		res.Dropped.IncompleteDay = out.Dropped.IncompleteDay
	}

	for _, s := range out.Summary {
		res.Summary = append(res.Summary, &v1.AlgoReviewSummary{
			Algo:         s.Algo,
			Label:        s.Label,
			Days:         s.Days,
			Pairs:        s.Pairs,
			MAE:          s.MAE,
			RMSE:         s.RMSE,
			MAPE:         s.MAPE,
			MAPEExcluded: s.MAPEExcluded,
			Bias:         s.Bias,
			Coverage:     s.Coverage,
		})
	}
	for _, d := range out.Daily {
		res.Daily = append(res.Daily, &v1.AlgoReviewDaily{
			Algo:         d.Algo,
			Date:         d.Date,
			Pairs:        d.Pairs,
			MAE:          d.MAE,
			MAPE:         d.MAPE,
			MAPEExcluded: d.MAPEExcluded,
			Coverage:     d.Coverage,
		})
	}
	for _, h := range out.Hourly {
		res.Hourly = append(res.Hourly, &v1.AlgoReviewHourly{
			Algo:  h.Algo,
			Hour:  h.Hour,
			Pairs: h.Pairs,
			MAE:   h.MAE,
			MAPE:  h.MAPE,
		})
	}
	for _, p := range out.Detail {
		res.Detail = append(res.Detail, &v1.AlgoReviewPoint{
			Algo:       p.Algo,
			Hour:       p.Hour,
			TargetTime: p.TargetTime,
			Predicted:  p.Predicted,
			Actual:     p.Actual,
			Q10:        p.Q10,
			Q50:        p.Q50,
			Q90:        p.Q90,
		})
	}
	return res, nil
}

// PostAlgoReviewRun 主动跑一次小时预测并写留痕。只执行、不返回指标；
// 跑完由 GetAlgoReview 按留痕统计。历史日按 as-of 锚点回放。
func (c *ControllerV1) PostAlgoReviewRun(ctx context.Context, req *v1.PostAlgoReviewRunReq) (res *v1.PostAlgoReviewRunRes, err error) {
	market := strings.ToLower(strings.TrimSpace(req.Market))
	if market == "" {
		market = "ahead"
	}
	if market != "ahead" && market != "real" {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "market 只能是 ahead 或 real")
	}

	from := strings.TrimSpace(req.From)
	to := strings.TrimSpace(req.To)
	start, perr := time.Parse("2006-01-02", from)
	if perr != nil {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "from 必须是 YYYY-MM-DD")
	}
	end, perr := time.Parse("2006-01-02", to)
	if perr != nil {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "to 必须是 YYYY-MM-DD")
	}
	if end.Before(start) {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "to 不能早于 from")
	}
	if end.Sub(start) > 30*24*time.Hour {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "一次最多跑 31 天，请分批执行")
	}
	loc := time.FixedZone("CST", 8*3600)
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	if end.After(today) {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "目标日不能晚于今天")
	}

	days, written, skipped := forecastlogic.RunHourForecastRange(ctx, from, to, []string{market})
	return &v1.PostAlgoReviewRunRes{Days: days, Written: written, Skipped: skipped}, nil
}
