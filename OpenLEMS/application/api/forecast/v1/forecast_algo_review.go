package v1

import "github.com/gogf/gf/v2/frame/g"

// GetAlgoReviewReq 小时电价预测算法回测（只读留痕，不重跑模型）。
//
// 口径：读 gd_price_forecast 里 grain=hour 的**当时留痕**，按 algo（timesfm / gbm）
// 分组，与实际值（gd_home_24h 的 ahead_price / realtime_price）逐点配对。
// 融合（fusion）= 同一目标时刻两个算法点预测的均值，展示时实时计算，不落库。
type GetAlgoReviewReq struct {
	g.Meta `path:"/forecast/algo-review" method:"get" tags:"电价预测" summary:"小时电价预测算法回测（留痕对比）" role:"user"`
	From   string `json:"from" v:"required" dc:"目标日起点 YYYY-MM-DD"`
	To     string `json:"to" v:"required" dc:"目标日终点 YYYY-MM-DD，跨度上限 366 天"`
	Market string `json:"market" dc:"ahead（默认）/ real"`
	Algos  string `json:"algos" dc:"算法，逗号分隔：timesfm,gbm,fusion；缺省全部"`
	// Date 非空时额外返回该目标日的 24 点明细（逐算法预测 vs 实际），供页面点某天查看。
	Date string `json:"date" dc:"可选：返回该目标日的 24 点明细 YYYY-MM-DD"`
}

// AlgoReviewSummary 单个算法在区间内的汇总指标。
type AlgoReviewSummary struct {
	Algo         string   `json:"algo" dc:"timesfm / gbm / fusion"`
	Label        string   `json:"label" dc:"展示名"`
	Days         int      `json:"days" dc:"有可配对点的目标日数"`
	Pairs        int      `json:"pairs" dc:"可配对的预测点数"`
	MAE          float64  `json:"mae" dc:"平均绝对误差"`
	RMSE         float64  `json:"rmse" dc:"均方根误差"`
	MAPE         float64  `json:"mape" dc:"平均绝对百分比误差（%，仅 |实际|>=1 的点）"`
	MAPEExcluded int      `json:"mapeExcluded" dc:"|实际|<1 被排除出 MAPE 的点数"`
	Bias         float64  `json:"bias" dc:"平均有符号误差（预测-实际），正=偏高"`
	Coverage     *float64 `json:"coverage" dc:"P10~P90 覆盖率（%）；融合无分位带，返回 null"`
}

// AlgoReviewDaily 按目标日的逐算法指标（页面画折线）。
type AlgoReviewDaily struct {
	Algo         string   `json:"algo"`
	Date         string   `json:"date" dc:"目标日 YYYY-MM-DD"`
	Pairs        int      `json:"pairs"`
	MAE          float64  `json:"mae"`
	MAPE         float64  `json:"mape"`
	MAPEExcluded int      `json:"mapeExcluded"`
	Coverage     *float64 `json:"coverage"`
}

// AlgoReviewHourly 24 个整点的误差画像（跨天聚合）。
type AlgoReviewHourly struct {
	Algo  string  `json:"algo"`
	Hour  int     `json:"hour" dc:"0~23"`
	Pairs int     `json:"pairs"`
	MAE   float64 `json:"mae"`
	MAPE  float64 `json:"mape"`
}

// AlgoReviewPoint 单日 24 点明细中的一条。
type AlgoReviewPoint struct {
	Algo       string   `json:"algo"`
	Hour       int      `json:"hour"`
	TargetTime string   `json:"targetTime" dc:"YYYY-MM-DD HH:00"`
	Predicted  float64  `json:"predicted"`
	Actual     *float64 `json:"actual" dc:"实际值；没有则为 null"`
	Q10        float64  `json:"q10"`
	Q50        float64  `json:"q50"`
	Q90        float64  `json:"q90"`
}

// AlgoReviewDropped 被剔除的样本，页面要显示，避免数字看起来「太干净」。
type AlgoReviewDropped struct {
	NoActual      int `json:"noActual" dc:"实际值缺失、无法配对的点数"`
	Backfilled    int `json:"backfilled" dc:"生成日晚于目标日、按事后回填剔除的点数"`
	IncompleteDay int `json:"incompleteDay" dc:"不足 24 点、整日弃用的算法-日组数"`
}

// GetAlgoReviewRes 算法回测结果。
type GetAlgoReviewRes struct {
	Market      string               `json:"market"`
	Unit        string               `json:"unit"`
	From        string               `json:"from"`
	To          string               `json:"to"`
	Algos       []string             `json:"algos"`
	Summary     []*AlgoReviewSummary `json:"summary"`
	Daily       []*AlgoReviewDaily   `json:"daily"`
	Hourly      []*AlgoReviewHourly  `json:"hourly"`
	Detail      []*AlgoReviewPoint   `json:"detail" dc:"仅 date 非空时返回"`
	Dropped     *AlgoReviewDropped   `json:"dropped"`
	DataAsOf    string               `json:"dataAsOf" dc:"实际值最新日期"`
	LagDays     int                  `json:"lagDays" dc:"实际值滞后天数；-1 表示拿不到"`
	TruthSource string               `json:"truthSource" dc:"真值来源说明"`
}
