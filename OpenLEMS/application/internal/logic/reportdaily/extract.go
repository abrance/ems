// Package reportdaily 日报核对（市场数据 → 日报核对）的写路径。
//
// 分层（与 spotreport 分开，别把 import 面糊在一起）：
//
//	采集   spotreport（微信 6 图，不动）
//	提取   spotreport.ExtractFromPages / RecognizePages（已有纯函数，只调用，不改输出集合）
//	落库   本包 → gd_report_daily（**绝不写旧链路那张表**，guard_test.go 守着）
//	官方   旧链路那张表（origin=pm-api）只读，作为核对右列
//	展示   /api/report-daily/reconcile → 页面 /scene/report-reconcile
//
// 口径由用户拍板，三条不可改：
//  1. OCR 落独立表 gd_report_daily，只存「分电源均价 + 申报结构」，绝不写旧链路的表；
//  2. 左日报原值 / 右官网直采 / 差值，不覆盖、不合并；
//  3. 缺日空态，不画 0 线、不用 0 填。
package reportdaily

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	v1 "application/api/reportdaily/v1"
	v1spot "application/api/spotreport/v1"
	"application/internal/logic/spotreport"
	"s_db"
	"s_db/s_db_basic"
	"s_db/s_db_model"

	"github.com/gogf/gf/v2/os/gtime"
)

// OriginOCR / OriginManual 本表允许的两种 origin。
// 这张表**没有**官方值：官方值永远在旧链路的表里（pm-api），右列另取一路。
const (
	OriginOCR    = "ocr"
	OriginManual = "manual"
)

// subsetDatasets D 子集允许的 dataset（其余一律不进新表）
var subsetDatasets = map[string]bool{
	spotreport.DatasetDecl:     true,
	spotreport.DatasetTrade:    true,
	spotreport.DatasetRealtime: true,
}

// subsetScopes 分电源均价 + 申报结构的对象
var subsetScopes = map[string]bool{
	spotreport.ScopeSellSide:  true, // 发电侧=全电源合计，唯一与官网同口径、能算差值的一行
	spotreport.ScopeCoal:      true,
	spotreport.ScopeGas:       true,
	spotreport.ScopeNewEnergy: true,
	// 申报结构的两行标签（表1(一) 的列组 B）
	spotreport.ScopeZero:     true,
	spotreport.ScopeAbove600: true,
}

// subsetDeclMetrics 申报结构保留的指标
var subsetDeclMetrics = map[string]bool{
	spotreport.MetricDeclVolume: true,
	spotreport.MetricCoalCap:    true,
	spotreport.MetricGasCap:     true,
	spotreport.MetricNewCap:     true,
	spotreport.MetricSelfDecl:   true,
	spotreport.MetricDefault:    true,
}

// SubsetMetrics 从模板提取结果里挑出 D 子集。
//
// 保留：
//   - 分电源均价：day_ahead_trade / realtime_trade × 发电侧/燃煤/燃气/新能源 × 平均电价
//   - 申报结构：day_ahead_decl × 发电侧/零价及以下/报价600以上 × 申报容量/煤机容量/气机容量/
//     新能源容量/自主申报台数/默认申报台数
//
// 其余（最高/最低电价、电价环比、summary 正文、节点价档位）一律丢弃：
// 那些是旧链路的事，核对页没有它们的右列可对。
func SubsetMetrics(in []spotreport.Metric) []spotreport.Metric {
	out := make([]spotreport.Metric, 0, len(in))
	for _, m := range in {
		if !subsetDatasets[m.Dataset] || !subsetScopes[m.Scope] {
			continue
		}
		switch m.Dataset {
		case spotreport.DatasetTrade, spotreport.DatasetRealtime:
			if m.Metric != spotreport.MetricAvgPrice {
				continue
			}
		case spotreport.DatasetDecl:
			if !subsetDeclMetrics[m.Metric] {
				continue
			}
		}
		out = append(out, m)
	}
	return out
}

// ExtractDay 解析一份已采集的日报，把 D 子集写进 gd_report_daily。
//
// force=false 时保留 origin=manual 的行（重跑不清人工修订）。
// 注意：本函数**不触碰**旧链路那张表 —— POST /api/spot-report/extract 仍在写它，
// 两条链路各写各的表，互不覆盖。
func ExtractDay(ctx context.Context, reportDate string, force bool) (*v1.ExtractDayRes, error) {
	report, err := s_db.GetSpotReportService().GetByDate(ctx, reportDate)
	if err != nil {
		return nil, err
	}
	if report == nil {
		return nil, fmt.Errorf("未找到 %s 的日报档案，请先采集", reportDate)
	}
	var pages []*v1spot.SpotPage
	if err := json.Unmarshal([]byte(report.Pages), &pages); err != nil {
		return nil, fmt.Errorf("日报页数据损坏：%v", err)
	}

	inputs, err := spotreport.RecognizePages(ctx, pages)
	if err != nil {
		return nil, err
	}
	extracted, err := spotreport.ExtractFromPages(reportDate, inputs)
	if err != nil {
		return nil, err
	}

	written, subsetCount, err := persistExtracted(ctx, reportDate, report.Title, extracted.Metrics, force)
	if err != nil {
		return nil, err
	}

	status := "ok"
	if subsetCount == 0 {
		status = "empty"
	}
	return &v1.ExtractDayRes{
		Date:           reportDate,
		TradeDate:      extracted.TradeDate,
		MetricRows:     written.Written,
		Preserved:      written.Preserved,
		SkippedFarther: written.SkippedFarther,
		Status:         status,
		// 模板的交叉校验告警照原样透出（它们解释「为什么这一天的某行看起来不对」），
		// 但不影响入库集合：入库只看上面的子集过滤。
		Warnings: nonNilStrings(extracted.Warnings),
	}, nil
}

// persistExtracted 把模板提取结果里 D 子集写进 gd_report_daily。
//
// 单独抽出来是为了可测：这是本包**唯一**的写路径，A2（「提取前后旧表行数与
// MAX(updated_at) 完全相同」）就是拿它跑真库验的 —— 写入口只要多碰一次旧表，
// 那条断言立刻红。参数收的是提取结果而不是日报档案，所以测试不用真跑 OCR。
func persistExtracted(ctx context.Context, reportDate, reportTitle string,
	metrics []spotreport.Metric, force bool) (s_db_basic.ReportDailyWriteResult, int, error) {
	subset := SubsetMetrics(metrics)
	rows := make([]*s_db_model.SReportDailyModel, 0, len(subset))
	now := gtime.Now()
	for _, m := range subset {
		rows = append(rows, &s_db_model.SReportDailyModel{
			RunDate:          m.RunDate,
			ReportDate:       reportDate,
			Dataset:          m.Dataset,
			Scope:            m.Scope,
			Metric:           m.Metric,
			Value:            m.Value,
			Unit:             m.Unit,
			Source:           m.Source,
			Origin:           OriginOCR,
			ExtractorVersion: spotreport.ExtractorVersion,
			ReportTitle:      reportTitle,
			CollectedAt:      now,
		})
	}
	written, err := s_db.GetReportDailyService().UpsertBatch(ctx, rows, force)
	if err != nil {
		return written, len(subset), err
	}
	return written, len(subset), nil
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// Backfill 一次性历史回填（只读旧表，单向、幂等）。
func Backfill(ctx context.Context) (*v1.BackfillRes, error) {
	res, err := s_db.GetReportDailyService().BackfillFromSpotMetric(ctx)
	if err != nil {
		return nil, err
	}
	return &v1.BackfillRes{
		Inserted: res.Inserted,
		Skipped:  res.Skipped,
		Conflict: res.Conflict,
	}, nil
}

// latestReconcileDate 两侧数据里较新的运行日；两侧都空时返回空串，由 normalizeRange 回落到今天。
func latestReconcileDate(ctx context.Context) (string, error) {
	svc := s_db.GetReportDailyService()
	report, err := svc.LatestRunDate(ctx)
	if err != nil {
		return "", err
	}
	official, err := svc.LatestOfficialRunDate(ctx)
	if err != nil {
		return "", err
	}
	if report > official {
		return report, nil
	}
	return official, nil
}

// normalizeRange 归一化区间：to 缺省时锚到今天（调用方先尝试数据锚点），往前 30 天，上限 730 天。
func normalizeRange(from, to string) (string, string, error) {
	to = strings.TrimSpace(to)
	from = strings.TrimSpace(from)
	if to == "" {
		to = gtime.Now().Format("Y-m-d")
	}
	t, err := gtime.StrToTime(to)
	if err != nil {
		return "", "", fmt.Errorf("结束日格式不对：%s", to)
	}
	if from == "" {
		from = t.AddDate(0, 0, -29).Format("Y-m-d")
	}
	f, err := gtime.StrToTime(from)
	if err != nil {
		return "", "", fmt.Errorf("起始日格式不对：%s", from)
	}
	if f.After(t) {
		return "", "", fmt.Errorf("起始日 %s 晚于结束日 %s", from, to)
	}
	if days := int(t.Sub(f).Hours()/24) + 1; days > 730 {
		return "", "", fmt.Errorf("区间 %d 天超过上限 730 天", days)
	}
	return from, to, nil
}

// LoadReconcile 核对页唯一数据源：左列读 gd_report_daily，右列读官方 pm-api，纯展示层派生。
//
// 不写任何表、不做 fallback、不把缺数补成 0：缺日报 → reportValue=null + MissingReport=true；
// 官网无此口径 → officialValue=null + compare=official_missing。
func LoadReconcile(ctx context.Context, from, to string) (*v1.ReconcileRes, error) {
	// 缺省右端锚到两侧数据里较新的那一天，不锚「今天」：
	// 日报/官网都可能滞后，拿今天当右端会让默认 30 天整段落在数据之后。
	if strings.TrimSpace(to) == "" {
		latest, err := latestReconcileDate(ctx)
		if err != nil {
			return nil, err
		}
		to = latest
	}
	from, to, err := normalizeRange(from, to)
	if err != nil {
		return nil, err
	}
	reportRows, err := s_db.GetReportDailyService().ListByRunDateRange(ctx, from, to)
	if err != nil {
		return nil, err
	}
	officialRows, err := s_db.GetReportDailyService().ListOfficialDaily(ctx, from, to)
	if err != nil {
		return nil, err
	}

	days, warnings := BuildReconcile(reportRows, officialRows)

	res := &v1.ReconcileRes{
		OfficialOrigin: "pm-api",
		From:           from,
		To:             to,
		Days:           make([]*v1.ReconcileDay, 0, len(days)),
		Warnings:       nonNilStrings(warnings),
	}
	for i := range days {
		res.Days = append(res.Days, &days[i])
	}
	// asOf 两侧各标各的：左列是「日报原值最新到哪天」，右列是「官方直采最新到哪天」。
	// 用区间内的最大值而不是库里的全局最新日，否则区间截断时会给出区间外的日期。
	for _, row := range reportRows {
		if row.RunDate > res.AsOfReport {
			res.AsOfReport = row.RunDate
		}
	}
	for _, row := range officialRows {
		if row.RunDate > res.AsOfOfficial {
			res.AsOfOfficial = row.RunDate
		}
	}
	return res, nil
}
