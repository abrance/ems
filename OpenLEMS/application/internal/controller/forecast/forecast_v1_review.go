package forecast

import (
	v1 "application/api/forecast/v1"
	forecastlogic "application/internal/logic/forecast"
	"context"
	"strings"

	"s_db"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// GetRecordedForecast 读取预测留痕。复盘的第一条腿：**当时**发出去的预测。
func (c *ControllerV1) GetRecordedForecast(ctx context.Context, req *v1.GetRecordedForecastReq) (res *v1.GetRecordedForecastRes, err error) {
	market := strings.ToLower(strings.TrimSpace(req.Market))
	if market == "" {
		market = "ahead"
	}
	grain := strings.ToLower(strings.TrimSpace(req.Grain))
	if grain == "" {
		grain = "intraday"
	}
	svc := s_db.GetPriceForecastService()
	rows, err := svc.ListByTargetRange(ctx, req.From, req.To, market, grain)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "读取预测留痕失败")
	}
	dates, err := svc.GeneratedDates(ctx, market, grain)
	if err != nil {
		dates = nil // 辅助信息，拿不到不影响主体
	}
	out := make([]*v1.RecordedForecastPoint, 0, len(rows))
	for _, r := range rows {
		out = append(out, &v1.RecordedForecastPoint{
			GeneratedOn: r.GeneratedOn,
			TargetTime:  r.TargetTime,
			Point:       r.Point,
			Q10:         r.Q10,
			Q50:         r.Q50,
			Q90:         r.Q90,
			Model:       r.Model,
			Unit:        r.Unit,
			Source:      r.Source,
		})
	}
	return &v1.GetRecordedForecastRes{Points: out, GeneratedDates: dates}, nil
}

// GetPriceBacktest 回测：用当前模型在历史数据上重跑。
// 与留痕的区别必须在页面上讲清 —— 回测数字会随模型/历史数据变化，不是当时发的预测。
func (c *ControllerV1) GetPriceBacktest(ctx context.Context, req *v1.GetPriceBacktestReq) (res *v1.GetPriceBacktestRes, err error) {
	market := strings.ToLower(strings.TrimSpace(req.Market))
	if market == "" {
		market = "ahead"
	}
	sampleDays := req.SampleDays
	if sampleDays <= 0 {
		sampleDays = 3
	}

	key := forecastlogic.BacktestCacheKey(market, req.From, req.To, sampleDays)
	bt, err := forecastlogic.BacktestCached(ctx, key, req.ForceRefresh, func(ctx context.Context) (*forecastlogic.SPriceBacktest, error) {
		return forecastlogic.BacktestPriceIntraday(ctx, market, req.From, req.To, sampleDays)
	})
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "回测失败")
	}

	points := make([]*v1.BacktestPoint, 0, len(bt.Points))
	for _, p := range bt.Points {
		points = append(points, &v1.BacktestPoint{
			TargetTime: p.TargetTime.Format("2006-01-02 15:04"),
			Predicted:  p.Predicted,
			Actual:     p.Actual,
			Q10:        p.Q10,
			Q50:        p.Q50,
			Q90:        p.Q90,
		})
	}
	return &v1.GetPriceBacktestRes{
		Market:      bt.Market,
		Grain:       bt.Grain,
		Unit:        bt.Unit,
		Model:       bt.Model,
		From:        bt.From,
		To:          bt.To,
		SampleDays:  bt.SampleDays,
		TargetDates: bt.TargetDates,
		Skipped:     bt.Skipped,
		Points:      points,
		TimeMs:      bt.TimeMs,
	}, nil
}
