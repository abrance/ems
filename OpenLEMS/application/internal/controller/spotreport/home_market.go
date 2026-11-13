package spotreport

import (
	"context"
	"time"

	v1 "application/api/spotreport/v1"
	"application/internal/logic/homemarket"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

// GetHome24h 官网主页 24 点分时（电价 + 负荷）。
//
// 与 /api/gd-metric/series（96 点第三方通道，已停更）和 /api/spot-market/board
// （披露接口日频）都不是一路：这是主页的 24 个整点，滞后 6 天左右，独立表 gd_home_24h。
func (c *ControllerV1) GetHome24h(ctx context.Context, req *v1.GetHome24hReq) (*v1.GetHome24hRes, error) {
	day, err := homemarket.LoadHome24h(ctx, req.Date)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "获取 24 点分时失败")
	}
	return &v1.GetHome24hRes{Home24hDay: day}, nil
}

// GetNodePrice 21 地市节点电价（高峰/低谷各 21 条）。
func (c *ControllerV1) GetNodePrice(ctx context.Context, req *v1.GetNodePriceReq) (*v1.GetNodePriceRes, error) {
	day, err := homemarket.LoadNodePrice(ctx, req.Date)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "获取地市节点价失败")
	}
	return &v1.GetNodePriceRes{NodePriceDay: day}, nil
}

// BackfillHomeMarket 手动回补官网主页两路数据（运维补洞）。
//
// 自动采集只能保证「首次启动补一次窗口 + 每轮采最新」：某天采集失败后，
// 下一轮的 CollectHome24h 会探到更新的日子停手，失败那天不会自己回来。
// 这个接口把那个洞补上，窗口与首次启动同一份实现（homemarket.RunBackfill）。
//
// 注意 days 只约束地市（T+0）；24 点源站 T-6 才发布，窗口由
// homemarket.Home24hWindowDays 单独换算，不会因为 days=7 就只剩一天有数。
//
// 同步阻塞：窗口上限 30 天 × 两路，最坏一个请求会跑很久，且没有并发保护
// （启动回补同时再调一次会对源站打双倍请求）。落库是 ON CONFLICT 幂等，不会写坏数据。
// 这是已知遗留项，改异步或加互斥另开 PR。
func (c *ControllerV1) BackfillHomeMarket(ctx context.Context, req *v1.BackfillHomeMarketReq) (*v1.BackfillHomeMarketRes, error) {
	days := req.Days
	if days <= 0 {
		// 缺省读配置，与自动采集保持一致：运维不传参数也不会补错窗口
		days = homemarket.ParseDays(g.Cfg().MustGet(ctx, "home_market.backfill_days", "7").String())
	}
	home24h, node := homemarket.RunBackfill(ctx, time.Now(), days)
	return &v1.BackfillHomeMarketRes{
		Days:        min(days, homemarket.MaxBackfillDays),
		Home24hDays: homemarket.Home24hWindowDays(days),
		Home24h:     home24h,
		NodePrice:   node,
	}, nil
}
