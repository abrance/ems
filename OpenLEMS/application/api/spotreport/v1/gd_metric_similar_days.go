package v1

import (
	"application/internal/logic/gdmetric"

	"github.com/gogf/gf/v2/frame/g"
)

// GetSimilarDaysReq 历史相似日查询。
//
// 相似度口径见 `internal/logic/gdmetric/similar.go` 的文件头：96 点价格/负荷形态的
// 加权皮尔逊相关，z-score 标准化后映射到 0~100%。
type GetSimilarDaysReq struct {
	g.Meta `path:"/gd-metric/similar-days" method:"get" tags:"电网日指标" summary:"历史相似日（96 点形态相关）" role:"user"`
	Date   string  `json:"date" dc:"目标运行日 YYYY-MM-DD，留空取最新可用"`
	Market string  `json:"market" dc:"ahead（默认）/ real：用日前还是实时价格曲线比"`
	Top    int     `json:"top" dc:"返回条数，默认 5，上限 20"`
	WPrice float64 `json:"wPrice" dc:"价格形态权重，默认 0.7；传 0 表示这次不看价格形态"`
	WLoad  float64 `json:"wLoad" dc:"负荷形态权重，默认 0.3；传 0 表示这次不看负荷形态"`
}

// GetSimilarDaysRes 相似日结果
type GetSimilarDaysRes struct {
	*gdmetric.SimilarDaysResult
}
