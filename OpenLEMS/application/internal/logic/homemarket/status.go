package homemarket

import (
	"context"
	"time"

	"s_db"
	"s_db/s_db_model"
)

// 状态端点：只回答「这一路现在能不能用、为什么不能用」，**不带任何数值**。
//
// 为什么单开一个端点而不塞进 /api/health：健康检查是给探针用的，一路采集滞后
// 不该把整个服务判成不健康（进程好好的，只是源站没发布）。这里返回细粒度状态，
// 由运维页面按需展示。
const (
	rowsExpectedHome24h   = 24 // 一天 24 个整点
	rowsExpectedNodePrice = 42 // 21 市 × 高峰/低谷
	maxLastErrorLen       = 500

	// expectedLagHome24h 24 点的正常滞后上限。源站 T-6 才有数，给到 8 天，
	// 超了就是「采集没跟上」而不是「源站还没发布」。
	expectedLagHome24h = 8
	// expectedLagNodePrice 地市 T+0 发布，允许 2 天（跨零点与重试）。
	expectedLagNodePrice = 2
	// stoppedEmptyRounds 同一个 target_date 连续多少次「HTTP 200 + 明确为空」，
	// 才认为最近这次是「源站没发」而不是偶发抖动。
	//
	// 注意它不是「全局连续几轮」：run 表的唯一键是 (target_date, dataset)，计数按日期累加，
	// 而 24 点每轮从今天往回探 14 天、健康的 T-6 滞后每轮都有 6 个日期必然为空，
	// 所以这个计数在完全正常的情况下也会涨到 3。停更判定必须叠加 lag 前置条件，
	// 见 deriveStatus 里 stopped 那一段。
	// shortcut: 没有「跨日期连续轮次」的计数（要做就得加表或加列，代价大于收益）；
	// 源站是否真停更以 lagDays 为准，这个计数只负责「这次是空，不是失败」。
	stoppedEmptyRounds = 3
)

// 四态（+ 正常）。状态不写进数据表：从 run 表 + 数据表现算，
// 避免「表里存的状态」和「表里实际的数据」长期分叉。
const (
	StateOK      = "ok"      // 有数且滞后在预期内
	StateNever   = "never"   // 从来没成功过，库里也没有任何数据
	StateFailed  = "failed"  // 最近一次尝试失败（拦截/网络/解析）
	StateLagging = "lagging" // 最近成功过，但最新数据已经超过预期滞后
	StateStopped = "stopped" // 连续 K 次「200 + 空」，源站大概率停更/改版
)

// DatasetStatus 一路采集的状态。字段与前端 status 卡片一一对应。
type DatasetStatus struct {
	// Dataset home24h / nodeprice
	Dataset string `json:"dataset"`
	// State never / failed / lagging / stopped / ok
	State string `json:"state"`
	// AsOf 最新有数据的那一天（YYYY-MM-DD）；库里一条都没有时为空串
	AsOf string `json:"asOf"`
	// FetchedAt 最近一次成功采集的时间（YYYY-MM-DD HH:MM:SS）；从没成功过时为空串
	FetchedAt string `json:"fetchedAt"`
	// LagDays asOf 距今多少天（口径固定为「相对今天」，与查询参数无关）；无数据时为 -1
	LagDays int `json:"lagDays"`
	// Rows asOf 那天的实际行数
	Rows int `json:"rows"`
	// RowsExpected 那天应该有的一行数（24 点=24，地市=42）
	RowsExpected int `json:"rowsExpected"`
	// LastError 最近一次失败的错误文案（只在该路最近一次尝试失败时给出）
	LastError string `json:"lastError"`
	// ErrorKind 失败类别 blocked / network / parse
	ErrorKind string `json:"errorKind"`
}

// MarketStatus 两路状态。
type MarketStatus struct {
	Home24h   DatasetStatus `json:"home24h"`
	NodePrice DatasetStatus `json:"nodePrice"`
}

// LoadMarketStatus 现算两路状态。now 由调用方给，方便测试固定「今天」。
func LoadMarketStatus(ctx context.Context, now time.Time) (*MarketStatus, error) {
	svc := s_db.GetHomeMarketService()

	hDate, err := svc.LatestHome24hDate(ctx)
	if err != nil {
		return nil, err
	}
	hRows, err := svc.ListHome24h(ctx, hDate)
	if err != nil {
		return nil, err
	}
	home24h, err := deriveStatus(ctx, s_db_model.DatasetHome24h, hDate, len(hRows),
		rowsExpectedHome24h, expectedLagHome24h, now)
	if err != nil {
		return nil, err
	}

	nDate, err := svc.LatestNodePriceDate(ctx)
	if err != nil {
		return nil, err
	}
	nRows, err := svc.ListNodePrice(ctx, nDate)
	if err != nil {
		return nil, err
	}
	nodePrice, err := deriveStatus(ctx, s_db_model.DatasetNodePrice, nDate, len(nRows),
		rowsExpectedNodePrice, expectedLagNodePrice, now)
	if err != nil {
		return nil, err
	}

	return &MarketStatus{Home24h: home24h, NodePrice: nodePrice}, nil
}

// deriveStatus 把 run 表和数据表现算成一个状态。
//
// 判定顺序有讲究：失败优先于「从未入库」—— 一个从没成功过、且最近一次被源站
// 400 挡掉的通道，说「从未入库」等于把故障说成还没开始。
func deriveStatus(ctx context.Context, dataset, asOf string, rows int,
	rowsExpected, expectedLag int, now time.Time) (DatasetStatus, error) {
	svc := s_db.GetHomeMarketService()
	st := DatasetStatus{
		Dataset:      dataset,
		AsOf:         asOf,
		LagDays:      -1,
		Rows:         rows,
		RowsExpected: rowsExpected,
	}
	if asOf != "" {
		st.LagDays = lagDays(asOf, now)
	}

	lastOK, err := svc.LatestOKFetchRun(ctx, dataset)
	if err != nil {
		return st, err
	}
	if lastOK != nil && lastOK.LastOkAt != nil && !lastOK.LastOkAt.IsZero() {
		st.FetchedAt = lastOK.LastOkAt.Format("Y-m-d H:i:s")
	}

	last, err := svc.LatestFetchRun(ctx, dataset)
	if err != nil {
		return st, err
	}

	switch {
	case last != nil && last.Status == s_db_model.FetchStatusFailed:
		st.State = StateFailed
		st.LastError = last.LastError
		st.ErrorKind = last.ErrorKind
	// stopped 必须排在 never 前面：全新环境（库里一条数都没有）碰上源站改版时，
	// asOf 同样是空的，先说「从未采集成功，请检查采集配置」会把用户引去查配置。
	//
	// 但「最近一次探测为空」本身不等于停更：24 点每轮从今天往回探 14 天，健康的
	// T-6 滞后每天都有 6 个日期必然返回空，跑满 3 轮就把今天那一行累加到 3。
	// 没有 lag 前置条件时，任何正常滞后 6 天的采集在 90 分钟内都会被判成「源站停更」，
	// 把用户引去查一个没坏的东西（线上 v1.0.70 就是这个形态）。
	// 源站真停更时 asOf 会冻结、lagDays 必然越过 expectedLag，所以前置条件不会漏报；
	// asOf 为空（LagDays = -1）单独放行，那是 #156 修过的全新环境形态。
	case last != nil && last.Status == s_db_model.FetchStatusEmpty &&
		last.ConsecutiveEmpties >= stoppedEmptyRounds &&
		(asOf == "" || st.LagDays > expectedLag):
		st.State = StateStopped
	case asOf == "":
		st.State = StateNever
	case st.LagDays > expectedLag:
		st.State = StateLagging
	default:
		st.State = StateOK
	}
	return st, nil
}
