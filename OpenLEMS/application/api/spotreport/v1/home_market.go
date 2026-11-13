package v1

import (
	"application/internal/logic/homemarket"

	"github.com/gogf/gf/v2/frame/g"
)

// GetHome24hReq 官网主页 24 点分时（日前/实时电价 + 系统负荷）。
//
// 与 /api/gd-metric（96 点第三方通道）和 /api/spot-market/board（披露接口日频）
// 都不是一路：这里是主页的 24 个整点，T-6 左右才有数，独立表 gd_home_24h。
type GetHome24hReq struct {
	g.Meta `path:"/gd-metric/home-24h" method:"get" tags:"电力市场" summary:"官网主页 24 点分时（电价+负荷）" role:"user"`
	Date   string `json:"date" dc:"运行日 YYYY-MM-DD，缺省为库里最新一天"`
}

type GetHome24hRes struct {
	*homemarket.Home24hDay
}

// GetNodePriceReq 21 地市节点电价（高峰/低谷）。
type GetNodePriceReq struct {
	g.Meta `path:"/gd-metric/node-price" method:"get" tags:"电力市场" summary:"21 地市节点电价（高峰/低谷）" role:"user"`
	Date   string `json:"date" dc:"运行日 YYYY-MM-DD，缺省为库里最新一天"`
}

type GetNodePriceRes struct {
	*homemarket.NodePriceDay
}
