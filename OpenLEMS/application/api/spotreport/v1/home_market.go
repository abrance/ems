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

// BackfillHomeMarketReq 手动触发一次两路回补（运维补洞用）。
//
// 为什么需要这个接口：自动采集只能保证「首次启动补一次窗口 + 每轮采最新」。
// 如果某天采集失败（源站临时挂掉、机器重启），下一轮的 CollectHome24h 探到
// 更新的日子就停手 —— 失败那天再也不会被重试，成为永久空洞。
// 这里让运维能指定窗口重跑，而不是只能进容器敲 SQL 或重启进程。
type BackfillHomeMarketReq struct {
	g.Meta `path:"/gd-metric/home-market/backfill" method:"post" tags:"电力市场" summary:"手动回补官网主页两路数据（24 点 + 地市节点价）" role:"admin"`
	Days   int `json:"days" d:"从今天往回回补的天数，缺省读配置 home_market.backfill_days（7），上限 30"`
}

type BackfillHomeMarketRes struct {
	Days      int            `json:"days" dc:"实际回补窗口（含当天）"`
	Home24h   map[string]int `json:"home24h" dc:"24 点：日期 → 写入点数（无数据的日子不出现）"`
	NodePrice map[string]int `json:"nodePrice" dc:"地市：日期 → 写入条数"`
}
