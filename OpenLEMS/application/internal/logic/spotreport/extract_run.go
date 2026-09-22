package spotreport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"time"

	v1 "application/api/spotreport/v1"
	"s_db"
	"s_db/s_db_basic"
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
	linesByPage := map[int][]OCRLine{}
	imageByPage := map[int]image.Image{}
	ocrMs := 0
	for _, page := range pages {
		imageBytes, err := downloadImage(ctx, page.URL)
		if err != nil {
			return nil, err
		}
		result, err := client.Recognize(ctx, imageBytes)
		if err != nil {
			return nil, fmt.Errorf("第 %d 页识别失败：%v", page.Page, err)
		}
		ocrMs += int(result.TimeMs)
		linesByPage[page.Page] = result.Results
		inputs = append(inputs, PageInput{
			Width:  float64(page.Width),
			Height: float64(page.Height),
			Lines:  result.Results,
		})
		// 图2-1 / 图2-2 的数值只在像素里，需要图像本身做颜色采样
		if page.Page == 2 || page.Page == 3 {
			if decoded, _, err := image.Decode(bytes.NewReader(imageBytes)); err == nil {
				imageByPage[page.Page] = decoded
			}
		}
	}

	extracted, err := ExtractFromPages(reportDate, inputs)
	if err != nil {
		return nil, err
	}
	extracted.Metrics = append(extracted.Metrics, sampleNodePriceMetrics(reportDate, &extracted.Warnings, imageByPage, linesByPage)...)
	// 与库中已有的同日指标交叉校验（表2 承载的是滞后运行日，正常情况下应与那天的表1(二) 一致）
	extracted.Warnings = append(extracted.Warnings, crossCheckStored(ctx, extracted)...)

	metrics := metricModels(reportDate, extracted.Metrics)
	overwritten, err := s_db.GetSpotExtractService().ReplaceMetrics(ctx, reportDate, metrics)
	if err != nil {
		return nil, err
	}
	extracted.Warnings = append(extracted.Warnings, overwriteWarnings(overwritten)...)

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
		editedAt := ""
		if m.EditedAt != nil {
			editedAt = m.EditedAt.Time.In(shanghaiLocation()).Format(time.RFC3339)
		}
		metrics = append(metrics, &v1.SpotMetric{
			RunDate: m.RunDate, Dataset: m.Dataset, Scope: m.Scope, Metric: m.Metric, Value: m.Value,
			Unit: m.Unit, AtTime: m.AtTime, TextValue: m.TextValue,
			MomPct: m.MomPct, SharePct: m.SharePct,
			Source: m.Source, Conf: m.Conf, Origin: m.Origin,
			EditedBy: m.EditedBy, EditedAt: editedAt,
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

// UpdateMetric 人工修订单个指标（异常兜底：自动提取为主，只改有问题的字段）
func UpdateMetric(ctx context.Context, req *v1.UpdateSpotMetricReq, operator string) (*v1.UpdateSpotMetricRes, error) {
	// 请求里显式给了修订人就用它，否则回落到会话角色
	if req.Operator != "" {
		operator = req.Operator
	}
	if operator == "" {
		operator = "admin"
	}
	model, err := s_db.GetSpotExtractService().UpdateMetricValue(ctx, s_db_basic.SpotMetricKey{
		ReportDate: req.Date, RunDate: req.RunDate, Dataset: req.Dataset,
		Scope: req.Scope, Metric: req.Metric,
	}, req.Value, operator)
	if err != nil {
		return nil, err
	}
	return &v1.UpdateSpotMetricRes{Metric: &v1.SpotMetric{
		Dataset: model.Dataset, Scope: model.Scope, Metric: model.Metric, Value: model.Value,
		Unit: model.Unit, AtTime: model.AtTime, MomPct: model.MomPct, SharePct: model.SharePct,
		Source: model.Source, Conf: model.Conf, Origin: model.Origin, EditedBy: model.EditedBy,
	}}, nil
}

// ListMetricAudit 指标变更审计
func ListMetricAudit(ctx context.Context, reportDate string, limit int) (*v1.GetSpotReportAuditRes, error) {
	models, err := s_db.GetSpotExtractService().ListMetricAudit(ctx, reportDate, limit)
	if err != nil {
		return nil, err
	}
	list := make([]*v1.SpotMetricAudit, 0, len(models))
	for _, m := range models {
		created := ""
		if m.CreatedAt != nil {
			created = m.CreatedAt.Time.In(shanghaiLocation()).Format(time.RFC3339)
		}
		list = append(list, &v1.SpotMetricAudit{
			RunDate: m.RunDate, Dataset: m.Dataset, Scope: m.Scope, Metric: m.Metric,
			OldValue: m.OldValue, NewValue: m.NewValue, Operation: m.Operation,
			Operator: m.Operator, CreatedAt: created,
		})
	}
	return &v1.GetSpotReportAuditRes{List: list}, nil
}

// overwriteWarnings 把「重跑覆盖了人工修订」转成可读告警。
// 重跑语义是以 OCR 为准，但人工判断不应无声消失：审计表留痕 + 这里显式提示。
func overwriteWarnings(overwritten []s_db_basic.SpotMetricOverwrite) []string {
	out := make([]string, 0, len(overwritten))
	for _, o := range overwritten {
		newText := "本次未提取到"
		if o.NewValue != nil {
			newText = fmt.Sprintf("%v", *o.NewValue)
		}
		source := o.Key.SourceLabel
		if source == "" {
			source = "-"
		}
		out = append(out, fmt.Sprintf("重跑覆盖人工修订：运行日 %s %s/%s（%s）人工值 %v → OCR 值 %s",
			o.Key.RunDate, o.Key.Scope, o.Key.Metric, source, o.OldValue, newText))
	}
	return out
}

// sampleNodePriceMetrics 采样图2-1（高峰）/ 图2-2（低谷）的地市电价档位。
//
// 这两张图的数值只存在于像素里（choropleth 填色），OCR 拿不到；采样结果标记为
// origin=cv，属于「预填」，需要人工在页面上确认。配色与图例不一致时不预填。
func sampleNodePriceMetrics(runDate string, warnings *[]string, images map[int]image.Image, lines map[int][]OCRLine) []Metric {
	specs := []struct {
		page    int
		dataset string
		source  string
	}{
		{2, DatasetNodePricePeak, "图2-1"},
		{3, DatasetNodePriceValley, "图2-2"},
	}
	var out []Metric
	for _, spec := range specs {
		img, okImg := images[spec.page]
		if !okImg {
			continue
		}
		buckets, warns := SampleMapBuckets(img, lines[spec.page])
		*warnings = append(*warnings, warns...)
		for _, b := range buckets {
			out = append(out, Metric{
				RunDate: runDate, Dataset: spec.dataset, Scope: b.City,
				Metric: MetricPriceBucket, Value: float64(b.Bucket), TextValue: b.Label,
				Source: spec.source, Conf: b.Conf, Origin: OriginCV,
			})
		}
	}
	return out
}

// metricModels 把提取结果映射为库模型。
//
// 单独抽出来是为了可测：映射漏字段（例如 origin）不会报错，只会让上游语义静默丢失
// —— 之前就漏过 origin，导致图像采样预填被当成普通 OCR 值入库。
func metricModels(reportDate string, metrics []Metric) []*s_db_model.SSpotMetricModel {
	out := make([]*s_db_model.SSpotMetricModel, 0, len(metrics))
	for _, m := range metrics {
		out = append(out, &s_db_model.SSpotMetricModel{
			ReportDate:       reportDate,
			RunDate:          m.RunDate,
			Dataset:          m.Dataset,
			Scope:            m.Scope,
			Metric:           m.Metric,
			Value:            m.Value,
			Unit:             m.Unit,
			AtTime:           m.AtTime,
			TextValue:        m.TextValue,
			MomPct:           m.MomPct,
			SharePct:         m.SharePct,
			Source:           m.Source,
			Conf:             m.Conf,
			Origin:           m.Origin,
			ExtractorVersion: ExtractorVersion,
		})
	}
	return out
}

// ConfirmReportMetrics 一键确认某份日报的采样/OCR 预填值（人工复核）
func ConfirmReportMetrics(ctx context.Context, reportDate, dataset, operator string) (*v1.ConfirmSpotMetricRes, error) {
	if operator == "" {
		operator = "admin"
	}
	count, err := s_db.GetSpotExtractService().ConfirmMetrics(ctx, reportDate, dataset, operator)
	if err != nil {
		return nil, err
	}
	return &v1.ConfirmSpotMetricRes{Confirmed: count}, nil
}
