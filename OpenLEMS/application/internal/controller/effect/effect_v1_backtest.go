package effect

import (
	v1 "application/api/effect/v1"
	effectlogic "application/internal/logic/effect"
	"context"
	"time"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// GetEffectBacktest 在同一段历史数据上回测各算法，输出有效率对比
func (c *ControllerV1) GetEffectBacktest(ctx context.Context, req *v1.GetEffectBacktestReq) (res *v1.GetEffectBacktestRes, err error) {
	start, end, err := parseDateRange(req.Start, req.End)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInvalidParameter, err, "时间范围非法")
	}

	summary, err := effectlogic.Backtest(ctx, start, end)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "算法回测失败")
	}

	return &v1.GetEffectBacktestRes{Backtest: toBacktestDTO(summary)}, nil
}

func toBacktestDTO(s *effectlogic.SBacktestSummary) *v1.EffectBacktest {
	if s == nil {
		return nil
	}
	results := make([]v1.BacktestAlgorithmResult, 0, len(s.Results))
	for _, r := range s.Results {
		results = append(results, v1.BacktestAlgorithmResult{
			Algorithm:         r.Algorithm,
			ChargeEnergy:      r.Savings.ChargeEnergy,
			DischargeEnergy:   r.Savings.DischargeEnergy,
			ChargeCost:        r.Savings.ChargeCost,
			DischargeRevenue:  r.Savings.DischargeRevenue,
			ArbitrageProfit:   r.Savings.ArbitrageProfit,
			ActualMaxDemand:   r.Savings.ActualMaxDemand,
			BaselineMaxDemand: r.Savings.BaselineMaxDemand,
			DemandSaving:      r.Savings.DemandSaving,
			TotalSaving:       r.Savings.TotalSaving,
			FinalSocPct:       r.FinalSocPct,
		})
	}
	return &v1.EffectBacktest{
		Start:        s.Start.Format(time.DateTime),
		End:          s.End.Format(time.DateTime),
		Points:       s.Points,
		GridDeviceId: s.GridDeviceId,
		EssDeviceIds: s.EssDeviceIds,
		DemandTarget: s.DemandTarget,
		DemandPrice:  s.DemandPrice,
		RatedPowerKW: s.Battery.RatedPowerKW,
		CapacityKWh:  s.Battery.CapacityKWh,
		Results:      results,
	}
}
