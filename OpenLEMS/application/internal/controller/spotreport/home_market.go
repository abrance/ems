package spotreport

import (
	"context"
	"strings"
	"time"

	v1 "application/api/spotreport/v1"
	"application/internal/logic/homemarket"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

// parseDateParam 校验 date 参数。
//
// 不做校验时传 2026/11/13 会走到「库里查不到」→ empty:true，和「源站还没发布」
// 完全一样，前端无法区分是填错还是没数据。格式错属于参数错误。
func parseDateParam(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if _, err := time.ParseInLocation("2006-01-02", raw, time.Local); err != nil {
		return "", gerror.NewCodef(gcode.CodeInvalidParameter, "date 必须是 YYYY-MM-DD（收到 %q）", raw)
	}
	return raw, nil
}

// GetHome24h 官网主页 24 点分时（电价 + 负荷）。
//
// 与 /api/gd-metric/series（96 点第三方通道，已停更）和 /api/spot-market/board
// （披露接口日频）都不是一路：这是主页的 24 个整点，滞后 6 天左右，独立表 gd_home_24h。
func (c *ControllerV1) GetHome24h(ctx context.Context, req *v1.GetHome24hReq) (*v1.GetHome24hRes, error) {
	date, err := parseDateParam(req.Date)
	if err != nil {
		return nil, err
	}
	day, err := homemarket.LoadHome24h(ctx, date)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "获取 24 点分时失败")
	}
	return &v1.GetHome24hRes{Home24hDay: day}, nil
}

// GetNodePrice 21 地市节点电价（高峰/低谷各 21 条）。
func (c *ControllerV1) GetNodePrice(ctx context.Context, req *v1.GetNodePriceReq) (*v1.GetNodePriceRes, error) {
	date, err := parseDateParam(req.Date)
	if err != nil {
		return nil, err
	}
	day, err := homemarket.LoadNodePrice(ctx, date)
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
// 同步阻塞：窗口上限 30 天 × 两路，最坏一个请求会跑很久，所以回补在逻辑层加了
// 包级互斥（homemarket.backfillMu）：同时再来一次直接返回「回补正在进行中」，
// 不排队、也不会对源站打双倍请求。
func (c *ControllerV1) BackfillHomeMarket(ctx context.Context, req *v1.BackfillHomeMarketReq) (*v1.BackfillHomeMarketRes, error) {
	days := req.Days
	if days <= 0 {
		// 缺省读配置，与自动采集保持一致：运维不传参数也不会补错窗口
		days = homemarket.ParseDays(g.Cfg().MustGet(ctx, "home_market.backfill_days", "7").String())
	}
	home24h, node, err := homemarket.RunBackfill(ctx, time.Now(), days)
	if err != nil {
		// 回补在跑属于「操作冲突」而不是参数错：CodeInvalidParameter 会让前端
		// 把提示挂在 days 输入框上，这里用 Invalid Operation。
		return nil, gerror.NewCode(gcode.CodeInvalidOperation, err.Error())
	}
	return &v1.BackfillHomeMarketRes{
		Days:        min(days, homemarket.MaxBackfillDays),
		Home24hDays: homemarket.Home24hWindowDays(days),
		Home24h:     home24h,
		NodePrice:   node,
	}, nil
}

// GetHomeMarketStatus 两路采集状态：只讲「能不能用」，不给数值。
//
// 四个非正常态各自对应一种处置：
//   - never   从没成功过：查配置/网络/是否被源站 400 挡死；
//   - failed  最近一次失败，errorKind 区分 blocked（防护）/ network / parse（改版）；
//   - stopped 连续 K 次「200 + 空」：源站停更或页面改版，别再等人肉盯；
//   - lagging 最近成功但数据滞后超预期：采集在跑但没采到新的。
func (c *ControllerV1) GetHomeMarketStatus(ctx context.Context, req *v1.GetHomeMarketStatusReq) (*v1.GetHomeMarketStatusRes, error) {
	st, err := homemarket.LoadMarketStatus(ctx, time.Now())
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "获取官网主页采集状态失败")
	}
	return &v1.GetHomeMarketStatusRes{MarketStatus: st}, nil
}
