package v1

import "github.com/gogf/gf/v2/frame/g"

// GetIntradayResearchReq 第三方 96 点分时（分时研究页）。
//
// 口径（2026-11-13 拍板）：来源是第三方、非官方；**不采集、不要电查查 token、
// 不加定时任务**，只读本地已有的冻结快照。官方披露没有 96 点粒度，
// 所以这条能力只能以「非官方研究数据」的形态存在，不参与复盘 MAE。
type GetIntradayResearchReq struct {
	g.Meta `path:"/research/intraday" method:"get" tags:"分时研究" summary:"第三方 96 点分时（只读，非官方）" role:"user"`
	From   string `json:"from" dc:"起始运行日 YYYY-MM-DD，缺省为截至日往前 6 天"`
	To     string `json:"to" dc:"结束运行日 YYYY-MM-DD，缺省为数据截至日（不是今天）"`
}

// IntradayResearchSource 数据源元信息（一图一源：一个响应只能有一个 source）。
type IntradayResearchSource struct {
	ID   string `json:"id" dc:"源标识，例如 gdmetric.frozen"`
	Name string `json:"name" dc:"给用户看的源名"`
	Kind string `json:"kind" dc:"official / thirdparty"`
}

type IntradayResearchPoint struct {
	Time  string  `json:"time" dc:"HH:MM，00:15 … 24:00"`
	Value float64 `json:"value" dc:"元/兆瓦时"`
}

type IntradayResearchDay struct {
	Date   string                   `json:"date"`
	Points []*IntradayResearchPoint `json:"points" dc:"该日 96 点，升序"`
}

type GetIntradayResearchRes struct {
	Source    *IntradayResearchSource `json:"source"`
	AsOf      string                  `json:"asOf" dc:"最后一天有实际值的日期；空串 = 本地完全没有数据"`
	LagDays   int                     `json:"lagDays" dc:"asOf 距今自然日数（上海时区），-1 = 未知"`
	Stale     bool                    `json:"stale" dc:"服务端判定：lagDays > 7 即为过期，前端不重算"`
	From      string                  `json:"from"`
	To        string                  `json:"to"`
	Days      []*IntradayResearchDay  `json:"days" dc:"区间内**有数据**的日期，只含实际值"`
	Missing   []string                `json:"missing" dc:"区间内没有入库的日期（页面渲染空态，不画 0 线）"`
	Available []string                `json:"available" dc:"本地缓存里全部有数据的日期（升序），供日期选择器灰显"`
}
