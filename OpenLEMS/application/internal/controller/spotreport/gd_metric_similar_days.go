package spotreport

import (
	v1 "application/api/spotreport/v1"
	"application/internal/logic/gdmetric"
	"context"
)

// GetSimilarDays 历史相似日。
//
// 这是**参考**，不产出预测值：页面拿它做「历史上像今天的日子长什么样」，
// 别把它当成「相似日取平均 = 预测」（那是另一套算法，得单独回测）。
func (c *ControllerV1) GetSimilarDays(ctx context.Context, req *v1.GetSimilarDaysReq) (res *v1.GetSimilarDaysRes, err error) {
	kind, err := gdmetric.NormalizeKind(req.Market)
	if err != nil {
		return nil, err
	}
	if kind == gdmetric.KindLoad {
		// 负荷是特征之一（横轴驱动），不能当被比较的价格曲线
		kind = gdmetric.KindDayAhead
	}
	result, err := gdmetric.SimilarDays(ctx, req.Date, kind, gdmetric.SimilarOptions{
		TopN:   req.Top,
		WPrice: req.WPrice,
		WLoad:  req.WLoad,
	})
	if err != nil {
		return nil, err
	}
	return &v1.GetSimilarDaysRes{SimilarDaysResult: result}, nil
}
