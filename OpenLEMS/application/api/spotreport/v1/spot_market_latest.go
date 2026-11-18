package v1

import "github.com/gogf/gf/v2/frame/g"

// GetSpotMarketLatestReq 信息披露页日频回看窗口的右端锚点。
//
// 单独一个接口而不是塞进 board：前端要先用它定默认区间，再按区间取 board。
// 让 board 自己在 date 为空时猜「最新日」会把「用户显式选的截止日」和「缺省锚点」
// 混成一个参数，测试与排障都分不清。
type GetSpotMarketLatestReq struct {
	g.Meta `path:"/spot-market/latest" method:"get" tags:"电力市场" summary:"广东电力市场日频最新运行日" role:"user"`
}

type GetSpotMarketLatestRes struct {
	// Date 最近一个有日前数据的运行日；空串表示库里一条都没有。
	Date string `json:"date" dc:"最近一个有日前数据的运行日；空串表示库里没有"`
}
