package v1

import "github.com/gogf/gf/v2/frame/g"

// PostAlgoReviewRunReq 主动触发一次「小时预测算法运行」。
//
// 与看统计解耦：这里只负责跑模型并写留痕（gd_price_forecast），不返回指标；
// 跑完后由算法回测接口按留痕统计。历史目标日按 as-of 锚点回放（锚点 = 目标日前
// 最新完整 24 点日），所以昨天/上周的数据现在就能补跑，不用等每日任务慢慢积累。
type PostAlgoReviewRunReq struct {
	g.Meta `path:"/forecast/algo-review/run" method:"post" tags:"电价预测" summary:"主动运行小时预测并写留痕" role:"user"`
	From   string `json:"from" v:"required" dc:"目标日起点 YYYY-MM-DD"`
	To     string `json:"to" v:"required" dc:"目标日终点 YYYY-MM-DD，跨度上限 31 天"`
	Market string `json:"market" dc:"ahead（默认）/ real"`
}

type PostAlgoReviewRunRes struct {
	Days    int            `json:"days" dc:"本次尝试的目标日数"`
	Written map[string]int `json:"written" dc:"市场 → 实际写入的留痕行数（TimesFM + GBM）"`
	Skipped []string       `json:"skipped" dc:"跳过/失败的目标日与原因"`
}
