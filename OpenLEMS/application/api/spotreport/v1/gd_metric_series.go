package v1

import (
	"application/internal/logic/gdmetric"

	"github.com/gogf/gf/v2/frame/g"
)

// GetGdMetricSeriesReq 电网日指标区间序列（96 点）
type GetGdMetricSeriesReq struct {
	g.Meta `path:"/gd-metric/series" method:"get" tags:"电网日指标" summary:"电网负荷/电价 15 分钟序列（区间）" role:"user"`
	From   string `json:"from" v:"required" dc:"起始日期 YYYY-MM-DD"`
	To     string `json:"to" v:"required" dc:"结束日期 YYYY-MM-DD"`
	Kind   string `json:"kind" dc:"类别：dayAhead(默认) / realtime / load"`
}

// GetGdMetricSeriesRes 区间序列
type GetGdMetricSeriesRes struct {
	Kind   string                  `json:"kind"`
	From   string                  `json:"from"`
	To     string                  `json:"to"`
	Points []*gdmetric.TimedSample `json:"points" dc:"按时间升序；区间内没有数据的日期直接跳过"`
	Days   []string                `json:"days" dc:"区间内实际有数据的日期（升序），供前端灰显无数据日期"`
}
