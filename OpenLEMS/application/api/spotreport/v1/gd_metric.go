package v1

import (
	"application/internal/logic/gdmetric"

	"github.com/gogf/gf/v2/frame/g"
)

type GetGdMetricReq struct {
	g.Meta `path:"/gd-metric" method:"get" tags:"电网日指标" summary:"电网负荷与现货电价日指标" role:"user"`
	From   string `json:"from" dc:"起始运行日 YYYY-MM-DD，缺省为最新日往前 30 天"`
	To     string `json:"to" dc:"结束运行日 YYYY-MM-DD，缺省为数据最新日"`
	Date   string `json:"date" dc:"当日 15 分钟曲线的运行日，缺省为最新日"`
	// AllowFrozen 显式读取**已停更**的第三方通道（历史演示用）。不传一律报停更错误。
	AllowFrozen bool `json:"allowFrozen" dc:"读已停更的第三方通道（历史演示）"`
}

type GetGdMetricRes struct {
	*gdmetric.Snapshot
}
