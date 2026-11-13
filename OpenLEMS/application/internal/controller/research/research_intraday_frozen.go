package research

import (
	"context"
	"regexp"

	v1 "application/api/research/v1"
	logic "application/internal/logic/research"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// GetIntradayResearch 第三方 96 点分时（**只读**：无采集、无定时、不落库）。
//
// 口径都在 logic/research 里定（asOf / missing / 空态），这里只做参数校验与 DTO 转换。
func (c *ControllerV1) GetIntradayResearch(ctx context.Context, req *v1.GetIntradayResearchReq) (*v1.GetIntradayResearchRes, error) {
	for _, v := range []string{req.From, req.To} {
		if v != "" && !datePattern.MatchString(v) {
			return nil, gerror.NewCode(gcode.CodeInvalidParameter, "日期格式无效，需 YYYY-MM-DD")
		}
	}
	if req.From != "" && req.To != "" && req.From > req.To {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "起始日期不能晚于结束日期")
	}

	r, err := logic.Load(ctx, req.From, req.To, nowFunc())
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "读取第三方分时快照失败")
	}

	res := &v1.GetIntradayResearchRes{
		Source: &v1.IntradayResearchSource{
			ID:   r.Source.ID,
			Name: r.Source.Name,
			Kind: r.Source.Kind,
		},
		AsOf:      r.AsOf,
		LagDays:   r.LagDays,
		Stale:     r.Stale,
		From:      r.From,
		To:        r.To,
		Days:      make([]*v1.IntradayResearchDay, 0, len(r.Days)),
		Missing:   r.Missing,
		Available: r.Available,
	}
	for _, d := range r.Days {
		pts := make([]*v1.IntradayResearchPoint, 0, len(d.Points))
		for _, p := range d.Points {
			pts = append(pts, &v1.IntradayResearchPoint{Time: p.Time, Value: p.Value})
		}
		res.Days = append(res.Days, &v1.IntradayResearchDay{Date: d.Date, Points: pts})
	}
	return res, nil
}
