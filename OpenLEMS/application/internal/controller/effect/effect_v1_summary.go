package effect

import (
	v1 "application/api/effect/v1"
	effectlogic "application/internal/logic/effect"
	"context"
	"time"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// GetEffectSummary 计算指定区间的储能效果（峰谷套利 + 需量节省）
func (c *ControllerV1) GetEffectSummary(ctx context.Context, req *v1.GetEffectSummaryReq) (res *v1.GetEffectSummaryRes, err error) {
	start, end, err := parseDateRange(req.Start, req.End)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInvalidParameter, err, "时间范围非法")
	}

	summary, err := effectlogic.Summary(ctx, start, end)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "计算储能效果失败")
	}

	return &v1.GetEffectSummaryRes{Summary: toDTO(summary)}, nil
}

// parseDateRange 解析 YYYY-MM-DD 日期区间为 [当日00:00, 次日00:00)；缺省为今日。
func parseDateRange(startDate, endDate string) (time.Time, time.Time, error) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 0, 1)

	if startDate != "" {
		t, err := time.ParseInLocation("2006-01-02", startDate, now.Location())
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		start = t
	}
	if endDate != "" {
		t, err := time.ParseInLocation("2006-01-02", endDate, now.Location())
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		end = t.AddDate(0, 0, 1)
	}
	return start, end, nil
}

func toDTO(s *effectlogic.SSummary) *v1.EffectSummary {
	if s == nil {
		return nil
	}
	return &v1.EffectSummary{
		Start:             s.Start.Format(time.DateTime),
		End:               s.End.Format(time.DateTime),
		Points:            s.Points,
		Algorithm:         s.Algorithm,
		GridDeviceId:      s.GridDeviceId,
		EssDeviceIds:      s.EssDeviceIds,
		DemandPrice:       s.DemandPrice,
		ChargeEnergy:      s.Savings.ChargeEnergy,
		DischargeEnergy:   s.Savings.DischargeEnergy,
		ChargeCost:        s.Savings.ChargeCost,
		DischargeRevenue:  s.Savings.DischargeRevenue,
		ArbitrageProfit:   s.Savings.ArbitrageProfit,
		ActualMaxDemand:   s.Savings.ActualMaxDemand,
		BaselineMaxDemand: s.Savings.BaselineMaxDemand,
		DemandSaving:      s.Savings.DemandSaving,
		TotalSaving:       s.Savings.TotalSaving,
	}
}
