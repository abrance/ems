package spotreport

import (
	v1 "application/api/spotreport/v1"
	"application/internal/logic/gdmetric"
	"context"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// GetGdMetricSeries 取区间内的 15 分钟序列（96 点/天）。
//
// 这是「预测复盘」拿来对比实际值的接口：预测来自 /api/forecast/price?grain=intraday，
// 实际值来自这里，两边的时间轴都是 15 分钟，才能逐点算误差。
func (c *ControllerV1) GetGdMetricSeries(ctx context.Context, req *v1.GetGdMetricSeriesReq) (res *v1.GetGdMetricSeriesRes, err error) {
	kind, err := gdmetric.NormalizeKind(req.Kind)
	if err != nil {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, err.Error())
	}
	points, err := gdmetric.GetSeries(ctx, req.From, req.To, kind)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "读取电网日指标序列失败")
	}
	days, err := gdmetric.AvailableDates(ctx, req.From, req.To)
	if err != nil {
		// 日期清单只是辅助信息，拿不到不影响序列本身
		days = nil
	}
	out := make([]*gdmetric.TimedSample, 0, len(points))
	for i := range points {
		p := points[i]
		out = append(out, &p)
	}
	return &v1.GetGdMetricSeriesRes{Kind: string(kind), From: req.From, To: req.To, Points: out, Days: days}, nil
}
