package v1

import (
	"application/internal/logic/spotmarket"

	"github.com/gogf/gf/v2/frame/g"
)

// GetSpotMarketBoardReq 信息披露页一次取完：日频全套 + 月频年频全套 + 基础统计。
//
// 为什么是一个接口而不是「按指标取数」的若干接口：页面要的是同一时刻的一张
// 快照（同一天的全部指标 + 同一窗口的环比同比），拆成多个请求会出现半新半旧的拼盘。
// 数据量：90 天日频约 800 行、月频年频约 700 行，一次返回即可。
type GetSpotMarketBoardReq struct {
	g.Meta `path:"/spot-market/board" method:"get" tags:"电力市场" summary:"广东电力市场官方指标（日频+月频年频+基础统计）" role:"user"`
	Date   string `json:"date" dc:"截止运行日 YYYY-MM-DD，缺省为今天"`
	Days   int    `json:"days" dc:"日频回看天数，缺省 90，上限 730"`
}

type GetSpotMarketBoardRes struct {
	*spotmarket.Board
}
