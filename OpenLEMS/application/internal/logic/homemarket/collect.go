// Package homemarket 官网主页两路市场数据的采集（24 点分时 + 地市节点价）。
//
// 为什么不复用 tools/gdpm（披露接口那套）：那是另一套端点、另一套口径。
// 本包打的是 pm.gd.csg.cn 的**主页**网关：
//
//	24 点   /portal/xhr/sso-portal/portal/home/trade/tradeSituation?date=D&type=1
//	地市     /portal/xhr/sso-portal/portal/home/marketEntitiesNum?date=D&model=2
//
// 直连 /sso-portal/... 会 400（动态防护），必须走 /portal/xhr/ 网关。
// 两者都匿名可取，不需要账号。
//
// 滞后口径（2026-11-13 实测）：
//   - 24 点：T-6 左右才有数（11-13 当天只能取到 11-07 及以前），所以采集器**从今天往回探**
//     找最近一个有数的日子，而不是硬取今天。
//   - 地市：T+0 可得（当天就有 42 条），所以除当天外要能按日期回补。
//
// 与 gd_spot_metric 的采集（tools/gdpm → gd_pm_fetch_run）**完全分离**：独立任务、
// 独立 cron、独立失败语义。一边挂了不影响另一边。
package homemarket

import (
	"context"
	"encoding/json"
	stdErrors "errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"s_db"
	"s_db/s_db_model"

	"common/c_log"
	"github.com/pkg/errors"
)

// gatewayBase 主页网关前缀。
//
// 必须走 /portal/xhr/：直连 /sso-portal/... 会 400（源站动态防护）。
// 是变量而不是常量，为了单测能把请求指向本地桩服务。
var gatewayBase = "https://pm.gd.csg.cn/portal/xhr/sso-portal/portal/home"

const (
	// SourceHome24h 24 点分时的来源角标（页面直接展示）
	SourceHome24h = "官网·24点"
	// SourceNodePrice 地市节点价的来源角标
	SourceNodePrice = "官网·节点价"

	dateLayout = "2006-01-02"
	// maxLookbackDays 24 点往回最多探几天。实测滞后 6 天左右，给到 14 天留余量；
	// 再往回探是给「服务端某几天整段缺失」兜底，不是常态。
	maxLookbackDays = 14
	// defaultBackfillDays 地市按日期回补的默认天数（brief 定 N=7）。
	//
	// 只用于地市（T+0），**不要**拿它当 24 点的窗口：24 点源站滞后 6 天，
	// 7 天窗口只剩 1 天有数，停机几天后中间全是空洞。24 点走 Home24hWindowDays。
	defaultBackfillDays = 7
	// requestTimeout 单次 HTTP 超时
	requestTimeout = 30 * time.Second
)

var httpClient = &http.Client{Timeout: requestTimeout}

// collectErrKind 采集失败的类别。
//
// 为什么要分类：源站的动态防护会返回 HTTP 400，网络抖动是连接错误，源站改字段是
// JSON 形状变化 —— 三者处置完全不同（前者要换请求方式/降频，中者等下一轮，后者要改代码）。
// 只有「请求成功且 payload 明确为空」才是「源站未发布」，不能和失败混成一句话。
type collectErrKind string

const (
	// ErrKindBlocked 源站/网关拦截（HTTP 非 200，常见动态防护 400）
	ErrKindBlocked collectErrKind = "blocked"
	// ErrKindNetwork 连不上、超时、读响应失败
	ErrKindNetwork collectErrKind = "network"
	// ErrKindParse 响应不是预期形状（JSON 解析失败、业务 code != 200）
	ErrKindParse collectErrKind = "parse"
)

// collectError 带类别的采集错误。类别只在本包内判定，对外只体现 Error() 文案。
type collectError struct {
	Kind collectErrKind
	Err  error
}

func (e *collectError) Error() string { return fmt.Sprintf("%s: %v", e.Kind, e.Err) }
func (e *collectError) Unwrap() error { return e.Err }

// classifyErr 把采集错误归成三类；未带类别的错误按 network 计（兜底）。
func classifyErr(err error) collectErrKind {
	if err == nil {
		return ""
	}
	var ce *collectError
	if stdErrors.As(err, &ce) {
		return ce.Kind
	}
	return ErrKindNetwork
}

// tradeSituationResp 24 点响应（只取用到的字段，源站还有 monthUsePower 等）
type tradeSituationResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		LatestUsePower     float64 `json:"latestUsePower"`
		TodayMeanPrevPrice float64 `json:"todaymeanPrevPrice"`
		TodayMeanPrice     float64 `json:"todaymeanPrice"`
		Details            []struct {
			PowerNum      float64 `json:"powerNum"`
			CurrentPrice  float64 `json:"currentPrice"`
			RealTimePrice float64 `json:"realTimePrice"`
			DateTime      string  `json:"dateTime"`
		} `json:"details"`
	} `json:"data"`
}

// nodePriceResp 地市节点价响应
type nodePriceResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		CurrentPirceAreaVOList []struct {
			Name  string  `json:"name"`
			Value float64 `json:"value"`
			Type  int     `json:"type"`
		} `json:"currentPirceAreaVOList"`
	} `json:"data"`
}

// getJSON 打网关并解析。源站把业务错误也写在 code 里（HTTP 200），所以必须看 code。
func getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return &collectError{Kind: ErrKindNetwork, Err: err}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "+
		"(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://pm.gd.csg.cn/portal/")
	req.Header.Set("Accept", "application/json, text/plain, */*")

	res, err := httpClient.Do(req)
	if err != nil {
		return &collectError{Kind: ErrKindNetwork, Err: errors.Wrapf(err, "请求失败 %s", url)}
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return &collectError{Kind: ErrKindNetwork, Err: errors.Wrap(err, "读取响应失败")}
	}
	if res.StatusCode != http.StatusOK {
		return &collectError{Kind: ErrKindBlocked, Err: errors.Errorf("HTTP %d（%s）", res.StatusCode, truncate(strings.TrimSpace(string(body)), 160))}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &collectError{Kind: ErrKindParse, Err: errors.Wrapf(err, "解析响应失败（%s）", truncate(strings.TrimSpace(string(body)), 160))}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// FetchHome24h 取某天的 24 点。该日源站没有数据时返回空切片（不是错误）。
func FetchHome24h(ctx context.Context, date string) ([]*s_db_model.SHome24hModel, error) {
	var resp tradeSituationResp
	url := fmt.Sprintf("%s/trade/tradeSituation?date=%s&type=1", gatewayBase, date)
	if err := getJSON(ctx, url, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 200 {
		return nil, &collectError{Kind: ErrKindParse, Err: errors.Errorf("源站返回 code=%d msg=%s", resp.Code, resp.Msg)}
	}
	rows := make([]*s_db_model.SHome24hModel, 0, len(resp.Data.Details))
	for _, d := range resp.Data.Details {
		// dateTime 形如 "2026-11-07 00:00:00"，取整点小时
		hour, err := hourOf(d.DateTime)
		if err != nil {
			// 单条时间格式不认识就跳过这一条，不整批失败 —— 但要说出来
			c_log.Warningf(ctx, "24 点分时时间格式无法解析，跳过：%q", d.DateTime)
			continue
		}
		rows = append(rows, &s_db_model.SHome24hModel{
			PriceDate:     date,
			Hour:          hour,
			PowerMw:       d.PowerNum,
			AheadPrice:    d.CurrentPrice,
			RealtimePrice: d.RealTimePrice,
			Source:        SourceHome24h,
		})
	}
	if err := guardBatch(ctx, "24 点分时", len(resp.Data.Details), len(rows), func(i int) bool {
		return rows[i].AheadPrice == 0 && rows[i].RealtimePrice == 0
	}); err != nil {
		return nil, err
	}
	return rows, nil
}

func hourOf(dateTime string) (int, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(dateTime), time.Local)
	if err != nil {
		return 0, err
	}
	return t.Hour(), nil
}

// FetchNodePrice 取某天的 21 地市节点价（高峰/低谷各 21 条）。
func FetchNodePrice(ctx context.Context, date string) ([]*s_db_model.SNodePriceModel, error) {
	var resp nodePriceResp
	url := fmt.Sprintf("%s/marketEntitiesNum?date=%s&model=2", gatewayBase, date)
	if err := getJSON(ctx, url, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 200 {
		return nil, &collectError{Kind: ErrKindParse, Err: errors.Errorf("源站返回 code=%d msg=%s", resp.Code, resp.Msg)}
	}
	rows := make([]*s_db_model.SNodePriceModel, 0, len(resp.Data.CurrentPirceAreaVOList))
	for _, item := range resp.Data.CurrentPirceAreaVOList {
		if item.Name == "" {
			continue
		}
		if item.Type != s_db_model.NodePricePeriodPeak && item.Type != s_db_model.NodePricePeriodValley {
			c_log.Warningf(ctx, "地市节点价出现未知时段 type=%d（%s），跳过", item.Type, item.Name)
			continue
		}
		// 源站只给中文名，编码靠静态表映射。表里没有就跳过并告警：写空编码会撞
		// 唯一键 (run_date, city_code, period_type)，21 个城市会挤成一行。
		code, ok := CityCodeOf(item.Name)
		if !ok {
			c_log.Warningf(ctx, "地市节点价出现未知地市 %q（无行政区划编码），跳过；请补 city_code.go", item.Name)
			continue
		}
		rows = append(rows, &s_db_model.SNodePriceModel{
			RunDate:    date,
			CityCode:   code,
			City:       item.Name,
			PeriodType: item.Type,
			Price:      item.Value,
			Source:     SourceNodePrice,
		})
	}
	if err := guardBatch(ctx, "地市节点价", len(resp.Data.CurrentPirceAreaVOList), len(rows), func(i int) bool {
		return rows[i].Price == 0
	}); err != nil {
		return nil, err
	}
	return rows, nil
}

// guardBatch 挡两种「结构合法、值没用」的批次。
//
// 源站把字段改名后，JSON 照样能解出来，只是价格只剩 0 值：code=200、行数对，
// 这条路径**不会失败**，于是 0 元被当成真实价格写进库，页面显示「今天全天电价 0 元」。
// 错误分类抓不到它（不是 blocked/network，也不是 code!=200），必须在这里单独判。
//
// 两种形态：①源站有 N 行但价格整批为 0；②源站有 N 行但一条都没解出来
// （dateTime/name 改名会让全部行被跳过），后者原本会静默退化成「源站没发布」。
//
// 价格判据只能是「整批全为 0」，不能用 <=0：线上库实测 gd_node_price 有合法负价
// （2026-11-08 湛江低谷 -8.523），而 gd_home_24h 的 192 行最小值 53.2、零值 0 行。
// 真实价格偶尔为 0 是可能的，整批为 0 不可能。
func guardBatch(ctx context.Context, what string, raw, valid int, isZero func(i int) bool) error {
	if raw == 0 {
		return nil // 源站明确没发布，不是本函数管的事
	}
	if valid == 0 {
		c_log.Warningf(ctx, "%s 返回了 %d 行但一条也没解出来，疑似字段改名", what, raw)
		return &collectError{Kind: ErrKindParse, Err: errors.Errorf(
			"%s 返回了 %d 行但没有一条可解析（已拒绝写库），疑似字段改名或页面改版", what, raw)}
	}
	for i := 0; i < valid; i++ {
		if !isZero(i) {
			return nil
		}
	}
	c_log.Warningf(ctx, "%s 返回了 %d 行但价格全为 0，疑似字段改名或页面改版", what, valid)
	return &collectError{Kind: ErrKindParse, Err: errors.Errorf(
		"%s 返回了 %d 行但全为 0，疑似字段改名或页面改版（已拒绝写库）", what, valid)}
}

// CollectHome24h 采集「最近一个有数据的 24 点」并落库。
//
// 从今天往回探：主页 24 点滞后 6 天左右，硬取今天会永远拿到空。
// 找到第一个有数的日子就采完停手（不批量补历史 —— 那是 backfill 的事）。
// 每探一天都写一条 run 记录（成功/空/失败），失败的日子一行数据都不写。
func CollectHome24h(ctx context.Context, today time.Time) (string, int, error) {
	base := today.In(time.Local)
	errCount, emptyDays := 0, 0
	var lastErr error
	for i := 0; i < maxLookbackDays; i++ {
		date := base.AddDate(0, 0, -i).Format(dateLayout)
		rows, err := FetchHome24h(ctx, date)
		if err != nil {
			// 单日失败不终止：往前一天再试，循环结束后再定性。
			errCount++
			lastErr = err
			c_log.Warningf(ctx, "采集 24 点分时 %s 失败[%s]：%+v", date, classifyErr(err), err)
			recordRun(ctx, s_db_model.DatasetHome24h, date, s_db_model.FetchStatusFailed,
				classifyErr(err), 0, rowsExpectedHome24h, err)
			continue
		}
		if len(rows) == 0 {
			emptyDays++
			recordRun(ctx, s_db_model.DatasetHome24h, date, s_db_model.FetchStatusEmpty,
				"", 0, rowsExpectedHome24h, nil)
			continue
		}
		n, err := s_db.GetHomeMarketService().UpsertHome24h(ctx, rows)
		if err != nil {
			recordRun(ctx, s_db_model.DatasetHome24h, date, s_db_model.FetchStatusFailed,
				"", 0, rowsExpectedHome24h, err)
			return date, n, err
		}
		recordRun(ctx, s_db_model.DatasetHome24h, date, s_db_model.FetchStatusOK,
			"", n, rowsExpectedHome24h, nil)
		return date, n, nil
	}
	// 有失败就不能返回 nil：nil 会在调度器里被写成「最近 14 天都没有数据（源站未发布）」，
	// 源站故障被记成「没发布」，建在这句日志上的告警全是假的。
	if errCount > 0 {
		kind := classifyErr(lastErr)
		return "", 0, &collectError{Kind: kind, Err: errors.Errorf(
			"最近 %d 天都没有取到数据（失败 %d 天、成功但为空 %d 天），最后一次：%v",
			maxLookbackDays, errCount, emptyDays, lastErr)}
	}
	return "", 0, nil
}

// BackfillHome24h 按日期回补 24 点（指定区间，逐日；无数据/失败的日子只记日志，不写数据表）。
func BackfillHome24h(ctx context.Context, from, to time.Time) (map[string]int, error) {
	out := map[string]int{}
	for d := from.In(time.Local); !d.After(to.In(time.Local)); d = d.AddDate(0, 0, 1) {
		date := d.Format(dateLayout)
		rows, err := FetchHome24h(ctx, date)
		if err != nil {
			c_log.Warningf(ctx, "回补 24 点分时 %s 失败：%+v", date, err)
			recordRun(ctx, s_db_model.DatasetHome24h, date, s_db_model.FetchStatusFailed,
				classifyErr(err), 0, rowsExpectedHome24h, err)
			continue
		}
		if len(rows) == 0 {
			recordRun(ctx, s_db_model.DatasetHome24h, date, s_db_model.FetchStatusEmpty,
				"", 0, rowsExpectedHome24h, nil)
			continue
		}
		n, err := s_db.GetHomeMarketService().UpsertHome24h(ctx, rows)
		if err != nil {
			return out, err
		}
		recordRun(ctx, s_db_model.DatasetHome24h, date, s_db_model.FetchStatusOK,
			"", n, rowsExpectedHome24h, nil)
		out[date] = n
	}
	return out, nil
}

// CollectNodePrice 采集某天的地市节点价并落库。返回写入条数。
func CollectNodePrice(ctx context.Context, date string) (int, error) {
	rows, err := FetchNodePrice(ctx, date)
	if err != nil {
		recordRun(ctx, s_db_model.DatasetNodePrice, date, s_db_model.FetchStatusFailed,
			classifyErr(err), 0, rowsExpectedNodePrice, err)
		return 0, err
	}
	if len(rows) == 0 {
		recordRun(ctx, s_db_model.DatasetNodePrice, date, s_db_model.FetchStatusEmpty,
			"", 0, rowsExpectedNodePrice, nil)
		return 0, nil
	}
	n, err := s_db.GetHomeMarketService().UpsertNodePrice(ctx, rows)
	if err != nil {
		recordRun(ctx, s_db_model.DatasetNodePrice, date, s_db_model.FetchStatusFailed,
			"", 0, rowsExpectedNodePrice, err)
		return 0, err
	}
	recordRun(ctx, s_db_model.DatasetNodePrice, date, s_db_model.FetchStatusOK,
		"", n, rowsExpectedNodePrice, nil)
	return n, nil
}

// BackfillNodePrice 回补最近 N 天的地市节点价（T+0 口径，含当天）。
//
// 失败只记日志、不写假数据：宁可页面上少一天，也不要拿别的日期的价格顶上。
func BackfillNodePrice(ctx context.Context, today time.Time, days int) map[string]int {
	if days <= 0 {
		days = defaultBackfillDays
	}
	out := map[string]int{}
	base := today.In(time.Local)
	for i := 0; i < days; i++ {
		date := base.AddDate(0, 0, -i).Format(dateLayout)
		n, err := CollectNodePrice(ctx, date)
		if err != nil {
			c_log.Warningf(ctx, "采集地市节点价 %s 失败（不补假数据）：%+v", date, err)
			continue
		}
		if n > 0 {
			out[date] = n
		}
	}
	return out
}

// recordRun 把一次抓取尝试落进 run 表（该表只记「为什么没数」，不影响数据表）。
//
// 写失败只记日志、不向上抛：数据已经落库的情况下，因为观测表写不进去
// 把整轮采集判成失败，是拿副表绑架主流程。
func recordRun(ctx context.Context, dataset, date, status string, kind collectErrKind,
	written, expected int, cause error) {
	row := &s_db_model.SFetchRunModel{
		TargetDate:   date,
		Dataset:      dataset,
		Status:       status,
		ErrorKind:    string(kind),
		RowsWritten:  written,
		RowsExpected: expected,
	}
	if cause != nil {
		row.LastError = truncate(cause.Error(), maxLastErrorLen)
	}
	if _, err := s_db.GetHomeMarketService().RecordFetchRun(ctx, row); err != nil {
		c_log.Warningf(ctx, "写采集运行记录失败（%s %s）：%+v", dataset, date, err)
	}
}

// Home24hWindowDays 把调用方给的回补天数换算成 24 点的窗口。
//
// 两路滞后不同，不能共用一个数字：24 点源站 T-6 才发布，7 天窗口里
// 只有最后一天有数，停机几天后中间全是空洞。所以 24 点窗口至少 maxLookbackDays
// （14 天，按「滞后 6 天 + 余量」定），调用方要更大就跟更大。
func Home24hWindowDays(days int) int {
	if days <= 0 {
		days = defaultBackfillDays
	}
	if days < maxLookbackDays {
		return maxLookbackDays
	}
	if days > MaxBackfillDays {
		return MaxBackfillDays
	}
	return days
}

// ParseDays 读配置里的回补天数，非法值回落默认。
func ParseDays(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return defaultBackfillDays
	}
	return n
}
