package forecast

import (
	v1 "application/api/forecast/v1"
	forecastlogic "application/internal/logic/forecast"
	"context"
	"fmt"
	"strings"

	"s_db"
	"s_db/s_db_model"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// GetRecordedForecast 读取预测留痕。复盘的第一条腿：**当时**发出去的预测。
//
// 真值走官方日频（与日频回测同一口径，见 LoadDailyActuals）：页面不能一边说
// 「实际值截至 2026-11-11」（回测腿）、一边说「截至 2026-10-02」（留痕腿）。
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

	res = &v1.GetRecordedForecastRes{GeneratedDates: dates}

	// 真值：只有日频留痕能与官方日均价 1:1 配对。
	// 96 点留痕没有官方分时真值（公开披露只有日频），配对不成立，宁可不给。
	var actualByDate map[string]float64
	if grain == s_db_model.ForecastGrainDay {
		if series, aerr := forecastlogic.LoadDailyActuals(ctx, market, fromDate(req.From), req.To); aerr != nil {
			// 真值读不到不是致命错误：留痕本身还在，页面要说「没有可配对的实际值」。
			res.ActualUnavailable = aerr.Error()
		} else {
			actualByDate = series.ByDate
			res.DataAsOf = series.DataAsOf
			res.LagDays = series.LagDays
			res.Stale = series.Stale
			res.TruthSource = series.TruthSource
		}
	} else {
		res.ActualUnavailable = "96 点留痕没有官方分时真值可配对（官方披露只有日频）"
	}

	out := make([]*v1.RecordedForecastPoint, 0, len(rows))
	for _, r := range rows {
		targetDate := fromDate(r.TargetTime)
		// 生成日晚于目标日 = 事后回填的历史值，不是「当时发出去的预测」。
		// 拿它冒充留痕，等于把「当前模型重跑」贴上「当时预测」的标签。
		if r.GeneratedOn > targetDate {
			res.Backfilled++
			continue
		}
		point := &v1.RecordedForecastPoint{
			GeneratedOn: r.GeneratedOn,
			TargetTime:  r.TargetTime,
			Point:       r.Point,
			Q10:         r.Q10,
			Q50:         r.Q50,
			Q90:         r.Q90,
			Model:       r.Model,
			Unit:        r.Unit,
			Source:      r.Source,
		}
		if v, ok := actualByDate[targetDate]; ok {
			point.Actual = &v
		} else {
			res.ActualMissing++
		}
		out = append(out, point)
	}
	res.Points = out
	return res, nil
}

// fromDate 取 YYYY-MM-DD 部分（目标时刻/目标日两种格式都能用）
func fromDate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > len("2006-01-02") {
		return strings.TrimSpace(s[:len("2006-01-02")])
	}
	return s
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

	grain, err := forecastlogic.ParseGrain(req.Grain)
	if err != nil {
		return nil, err
	}

	// 粒度决定「真值从哪来」，这是本页诚实性的根：
	//   day      —— 官方直采日均价，随市场每天更新（默认）
	//   intraday —— 第三方 CSV 的 96 点，通道已停更，只能当历史演示
	if grain == s_db_model.ForecastGrainIntraday {
		key := forecastlogic.BacktestCacheKey(market, req.From, req.To, sampleDays)
		bt, err := forecastlogic.BacktestCached(ctx, key, req.ForceRefresh, func(ctx context.Context) (*forecastlogic.SPriceBacktest, error) {
			return forecastlogic.BacktestPriceIntraday(ctx, market, req.From, req.To, sampleDays)
		})
		if err != nil {
			return nil, gerror.WrapCode(gcode.CodeInternalError, err, "回测失败")
		}
		// 服务端兜一层：真值断了就报错，不静默在一个几周前的区间上算出漂亮数字。
		if bt.Stale && !req.AllowStale {
			return nil, gerror.NewCode(v1.CodePriceTruthStale, priceTruthStaleMessage(bt.DataAsOf, bt.LagDays))
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
			Market:        bt.Market,
			Grain:         bt.Grain,
			Unit:          bt.Unit,
			Model:         bt.Model,
			From:          bt.From,
			To:            bt.To,
			SampleDays:    bt.SampleDays,
			TargetDates:   bt.TargetDates,
			Skipped:       bt.Skipped,
			Points:        points,
			ActualMissing: bt.ActualMissing,
			TimeMs:        bt.TimeMs,
			DataAsOf:      bt.DataAsOf,
			LagDays:       bt.LagDays,
			Stale:         bt.Stale,
			TruthSource:   bt.TruthSource,
		}, nil
	}

	key := forecastlogic.DailyBacktestCacheKey(market, req.From, req.To, sampleDays)
	bt, err := forecastlogic.DailyBacktestCached(ctx, key, req.ForceRefresh, func(ctx context.Context) (*forecastlogic.SDailyBacktest, error) {
		return forecastlogic.BacktestPriceDaily(ctx, market, req.From, req.To, sampleDays)
	})
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "回测失败")
	}
	if bt.Stale && !req.AllowStale {
		return nil, gerror.NewCode(v1.CodePriceTruthStale, priceTruthStaleMessage(bt.DataAsOf, bt.LagDays))
	}

	points := make([]*v1.BacktestPoint, 0, len(bt.Points))
	for _, p := range bt.Points {
		points = append(points, &v1.BacktestPoint{
			TargetTime: p.TargetDate,
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
		HistoryDays: bt.HistoryDays,
		TargetDates: bt.TargetDates,
		Skipped:     bt.Skipped,
		Points:      points,
		TimeMs:      bt.TimeMs,
		DataAsOf:    bt.DataAsOf,
		LagDays:     bt.LagDays,
		Stale:       bt.Stale,
		TruthSource: bt.TruthSource,
	}, nil
}

// priceTruthStaleMessage 说清「哪一天、滞后多少天、怎么办」，页面直接展示这段话。
func priceTruthStaleMessage(dataAsOf string, lagDays int) string {
	if dataAsOf == "" {
		return "实际出清价不可用（读不到实际值），回测无法给出可信结果；确认数据源后重试"
	}
	return fmt.Sprintf("实际出清价只到 %s（滞后 %d 天，超过允许的 %d 天）。回测结果只能代表该历史区间，"+
		"不代表近期表现；带 allowStale=true 可查看", dataAsOf, lagDays, forecastlogic.MaxBacktestLagDays)
}
