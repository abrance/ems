package spotreport

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	v1 "application/api/spotreport/v1"
	"s_db"
	"s_db/s_db_model"

	"github.com/gogf/gf/v2/os/gtime"
)

// ExtractReport 对已采集的日报执行 OCR + 模板提取，并把结果落库。
//
// 手动触发（信息管理 → 解析数据）；采集接口本身不依赖 OCR，因此 OCR 服务不可用时
// 只是解析失败，不影响日报图片档案。
func ExtractReport(ctx context.Context, reportDate string) (*v1.ExtractSpotReportRes, error) {
	model, err := s_db.GetSpotReportService().GetByDate(ctx, reportDate)
	if err != nil {
		return nil, err
	}
	if model == nil {
		return nil, fmt.Errorf("未找到 %s 的日报档案，请先采集", reportDate)
	}

	var pages []*v1.SpotPage
	if err := json.Unmarshal([]byte(model.Pages), &pages); err != nil {
		return nil, fmt.Errorf("日报页数据损坏：%v", err)
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("日报没有可用页面")
	}

	client, err := NewOCRClientFromEnv()
	if err != nil {
		return nil, err
	}

	inputs := make([]PageInput, 0, len(pages))
	ocrMs := 0
	for _, page := range pages {
		image, err := downloadImage(ctx, page.URL)
		if err != nil {
			return nil, err
		}
		result, err := client.Recognize(ctx, image)
		if err != nil {
			return nil, fmt.Errorf("第 %d 页识别失败：%v", page.Page, err)
		}
		ocrMs += int(result.TimeMs)
		inputs = append(inputs, PageInput{
			Width:  float64(page.Width),
			Height: float64(page.Height),
			Lines:  result.Results,
		})
	}

	extracted, err := ExtractFromPages(reportDate, inputs)
	if err != nil {
		return nil, err
	}
	// 与库中已有的同日指标交叉校验（表2 承载的是滞后运行日，正常情况下应与那天的表1(二) 一致）
	extracted.Warnings = append(extracted.Warnings, crossCheckStored(ctx, extracted)...)

	metrics := make([]*s_db_model.SSpotMetricModel, 0, len(extracted.Metrics))
	for _, m := range extracted.Metrics {
		metrics = append(metrics, &s_db_model.SSpotMetricModel{
			ReportDate:       reportDate,
			RunDate:          m.RunDate,
			Dataset:          m.Dataset,
			Scope:            m.Scope,
			Metric:           m.Metric,
			Value:            m.Value,
			Unit:             m.Unit,
			AtTime:           m.AtTime,
			MomPct:           m.MomPct,
			SharePct:         m.SharePct,
			Source:           m.Source,
			Conf:             m.Conf,
			ExtractorVersion: ExtractorVersion,
		})
	}
	if err := s_db.GetSpotExtractService().ReplaceMetrics(ctx, reportDate, metrics); err != nil {
		return nil, err
	}

	status := s_db_model.SpotExtractStatusOk
	if len(metrics) == 0 {
		status = s_db_model.SpotExtractStatusFailed
	} else if len(extracted.Warnings) > 0 {
		status = s_db_model.SpotExtractStatusPartial
	}
	warningsJSON, _ := json.Marshal(extracted.Warnings)
	payloadJSON, _ := json.Marshal(map[string]any{
		"runDate":   extracted.RunDate,
		"tradeDate": extracted.TradeDate,
		"prose":     extracted.Prose,
		"metrics":   extracted.Metrics,
		"ocrModel":  client.Model(),
	})

	now := gtime.Now()
	if err := s_db.GetSpotExtractService().UpsertRun(ctx, &s_db_model.SSpotExtractRunModel{
		ReportDate:       reportDate,
		Status:           status,
		OcrModel:         client.Model(),
		PageCount:        len(inputs),
		OcrMs:            ocrMs,
		Warnings:         string(warningsJSON),
		Payload:          string(payloadJSON),
		ExtractorVersion: ExtractorVersion,
		UpdatedAt:        now,
	}); err != nil {
		return nil, err
	}

	return &v1.ExtractSpotReportRes{
		Date:        reportDate,
		TradeDate:   extracted.TradeDate,
		MetricCount: len(metrics),
		Status:      status,
		Warnings:    extracted.Warnings,
	}, nil
}

// GetReportData 读取已落库的结构化数据
func GetReportData(ctx context.Context, reportDate string) (*v1.GetSpotReportDataRes, error) {
	models, err := s_db.GetSpotExtractService().ListMetrics(ctx, reportDate)
	if err != nil {
		return nil, err
	}
	metrics := make([]*v1.SpotMetric, 0, len(models))
	tradeDate := ""
	for _, m := range models {
		metrics = append(metrics, &v1.SpotMetric{
			Dataset: m.Dataset, Scope: m.Scope, Metric: m.Metric, Value: m.Value,
			Unit: m.Unit, AtTime: m.AtTime, MomPct: m.MomPct, SharePct: m.SharePct,
			Source: m.Source, Conf: m.Conf,
		})
		if m.RunDate != reportDate && m.Dataset != DatasetSummary && tradeDate == "" {
			tradeDate = m.RunDate
		}
	}

	res := &v1.GetSpotReportDataRes{Date: reportDate, TradeDate: tradeDate, Metrics: metrics}
	run, err := s_db.GetSpotExtractService().GetRun(ctx, reportDate)
	if err != nil {
		return nil, err
	}
	if run != nil {
		var warnings []string
		if run.Warnings != "" {
			_ = json.Unmarshal([]byte(run.Warnings), &warnings)
		}
		updated := ""
		if run.UpdatedAt != nil {
			updated = run.UpdatedAt.Time.In(shanghaiLocation()).Format(time.RFC3339)
		}
		res.Extract = &v1.SpotExtractInfo{
			Status: run.Status, OcrModel: run.OcrModel, PageCount: run.PageCount,
			OcrMs: run.OcrMs, Warnings: warnings, UpdatedAt: updated,
			ExtractorV: run.ExtractorVersion,
		}
	}
	return res, nil
}

// crossCheckStored 用库中已有的同一运行日指标校验滞后数据（表2(一) vs 那天日报的表1(二)）
func crossCheckStored(ctx context.Context, extracted *ExtractResult) []string {
	stored, err := s_db.GetSpotExtractService().ListMetricsByRunDate(ctx, extracted.TradeDate)
	if err != nil || len(stored) == 0 {
		return nil
	}
	index := map[string]float64{}
	for _, m := range stored {
		if m.Dataset == DatasetTrade && m.Source == SourceTrade {
			index[m.Scope+"|"+m.Metric] = m.Value
		}
	}
	var warnings []string
	for _, m := range extracted.Metrics {
		if m.Source != SourceTradeLag || m.Metric == MetricMom {
			continue
		}
		key := m.Scope + "|" + m.Metric
		previous, ok := index[key]
		if !ok {
			continue
		}
		if abs(previous-m.Value) > 0.05 {
			warnings = append(warnings, fmt.Sprintf(
				"滞后运行日 %s 的 %s/%s=%v 与当日日报记录=%v 不一致", extracted.TradeDate, m.Scope, m.Metric, m.Value, previous))
		}
	}
	return warnings
}
