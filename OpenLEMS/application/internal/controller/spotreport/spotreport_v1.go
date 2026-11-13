package spotreport

import (
	"context"
	"regexp"

	v1 "application/api/spotreport/v1"
	"application/internal/logic/gdmetric"
	logic "application/internal/logic/spotreport"
	"s_db"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func (c *ControllerV1) ListSpotReport(ctx context.Context, req *v1.ListSpotReportReq) (*v1.ListSpotReportRes, error) {
	models, err := s_db.GetSpotReportService().List(ctx)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "获取现货日报列表失败")
	}
	list := make([]*v1.SpotReport, 0, len(models))
	counts, err := s_db.GetSpotExtractService().CountMetricsByReport(ctx)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "统计复核状态失败")
	}
	for _, model := range models {
		dto := logic.ToDTO(model)
		c := counts[dto.Date]
		dto.PendingCount, dto.ConfirmedCount = c.Pending, c.Confirmed
		list = append(list, dto)
	}
	return &v1.ListSpotReportRes{List: list}, nil
}

func (c *ControllerV1) GetHomeSpotReport(ctx context.Context, req *v1.GetHomeSpotReportReq) (*v1.GetHomeSpotReportRes, error) {
	models, err := s_db.GetSpotReportService().List(ctx)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "获取现货日报失败")
	}
	dates := make([]string, 0, len(models))
	for _, model := range models {
		dates = append(dates, model.RunDate)
	}
	if len(models) == 0 {
		return &v1.GetHomeSpotReportRes{Empty: true, Dates: dates}, nil
	}
	yesterday := logic.Yesterday()
	chosen, err := s_db.GetSpotReportService().GetByDate(ctx, yesterday)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "获取现货日报失败")
	}
	if chosen == nil {
		chosen, err = s_db.GetSpotReportService().GetLatest(ctx)
		if err != nil {
			return nil, gerror.WrapCode(gcode.CodeInternalError, err, "获取现货日报失败")
		}
	}
	return &v1.GetHomeSpotReportRes{
		Empty:  false,
		Date:   chosen.RunDate,
		Dates:  dates,
		Report: logic.ToDTO(chosen),
	}, nil
}

func (c *ControllerV1) GetSpotReportDetail(ctx context.Context, req *v1.GetSpotReportDetailReq) (*v1.GetSpotReportDetailRes, error) {
	if !datePattern.MatchString(req.Date) {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "运行日格式无效")
	}
	model, err := s_db.GetSpotReportService().GetByDate(ctx, req.Date)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "获取现货日报失败")
	}
	if model == nil {
		return nil, gerror.NewCode(gcode.CodeNotFound, "未找到该日档案")
	}
	return &v1.GetSpotReportDetailRes{Report: logic.ToDTO(model)}, nil
}

func (c *ControllerV1) CollectSpotReport(ctx context.Context, req *v1.CollectSpotReportReq) (*v1.CollectSpotReportRes, error) {
	report, err := logic.Collect(ctx, req.URL)
	if err != nil {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, err.Error())
	}
	return &v1.CollectSpotReportRes{Report: report}, nil
}

// ExtractSpotReport 解析日报数据（OCR + 模板提取）
func (c *ControllerV1) ExtractSpotReport(ctx context.Context, req *v1.ExtractSpotReportReq) (*v1.ExtractSpotReportRes, error) {
	if !datePattern.MatchString(req.Date) {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "运行日格式无效")
	}
	res, err := logic.ExtractReport(ctx, req.Date, req.Force)
	if err != nil {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, err.Error())
	}
	return res, nil
}

// GetSpotReportData 获取日报结构化数据
func (c *ControllerV1) GetSpotReportData(ctx context.Context, req *v1.GetSpotReportDataReq) (*v1.GetSpotReportDataRes, error) {
	if !datePattern.MatchString(req.Date) {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "运行日格式无效")
	}
	res, err := logic.GetReportData(ctx, req.Date)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "获取日报数据失败")
	}
	return res, nil
}

// UpdateSpotMetric 人工修订单个指标（管理员）
func (c *ControllerV1) UpdateSpotMetric(ctx context.Context, req *v1.UpdateSpotMetricReq) (*v1.UpdateSpotMetricRes, error) {
	if !datePattern.MatchString(req.Date) || !datePattern.MatchString(req.RunDate) {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "运行日格式无效")
	}
	operator := ""
	if r := g.RequestFromCtx(ctx); r != nil {
		operator = r.Session.MustGet("role").String()
	}
	res, err := logic.UpdateMetric(ctx, req, operator)
	if err != nil {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, err.Error())
	}
	return res, nil
}

// GetSpotReportAudit 指标变更审计（管理员）
func (c *ControllerV1) GetSpotReportAudit(ctx context.Context, req *v1.GetSpotReportAuditReq) (*v1.GetSpotReportAuditRes, error) {
	if !datePattern.MatchString(req.Date) {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "运行日格式无效")
	}
	res, err := logic.ListMetricAudit(ctx, req.Date, req.Limit)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "获取审计失败")
	}
	return res, nil
}

// ConfirmSpotMetric 确认预填值（管理员一键复核）
func (c *ControllerV1) ConfirmSpotMetric(ctx context.Context, req *v1.ConfirmSpotMetricReq) (*v1.ConfirmSpotMetricRes, error) {
	if !datePattern.MatchString(req.Date) {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "运行日格式无效")
	}
	operator := req.Operator
	if operator == "" {
		if r := g.RequestFromCtx(ctx); r != nil {
			operator = r.Session.MustGet("role").String()
		}
	}
	res, err := logic.ConfirmReportMetrics(ctx, req.Date, req.Dataset, operator)
	if err != nil {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, err.Error())
	}
	return res, nil
}

// BatchExtractSpotReport 批量重跑解析（后台任务，立即返回任务 id）
func (c *ControllerV1) BatchExtractSpotReport(ctx context.Context, req *v1.BatchExtractReq) (*v1.BatchExtractRes, error) {
	for _, d := range []string{req.From, req.To} {
		if d != "" && !datePattern.MatchString(d) {
			return nil, gerror.NewCode(gcode.CodeInvalidParameter, "日报日格式无效")
		}
	}
	res, err := logic.StartBatchExtract(ctx, req.From, req.To, req.IncludeConfirmed, req.DryRun)
	if err != nil {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, err.Error())
	}
	return res, nil
}

// GetBatchExtractStatus 批量重跑进度
func (c *ControllerV1) GetBatchExtractStatus(ctx context.Context, req *v1.BatchExtractStatusReq) (*v1.BatchExtractStatusRes, error) {
	res, err := logic.GetBatchExtract(req.Id)
	if err != nil {
		return nil, gerror.NewCode(gcode.CodeNotFound, err.Error())
	}
	return res, nil
}

// GetSpotReportTrend 现货指标趋势：按运行日分桶（日/周/月）聚合，只看已确认值。
// 缺省区间「本月 1 日 → 今天」，粒度按跨度推断；具体口径见 logic/spotreport/trend.go。
func (c *ControllerV1) GetSpotReportTrend(ctx context.Context, req *v1.GetSpotReportTrendReq) (*v1.GetSpotReportTrendRes, error) {
	from, to, err := logic.NormalizeTrendRange(req.From, req.To)
	if err != nil {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, err.Error())
	}
	grain := logic.DefaultTrendGrain(from, to)
	if parsed, ok := logic.ParseTrendGrain(req.Grain); ok {
		grain = parsed
	}
	service := s_db.GetSpotExtractService()
	models, err := service.ListMetricsByRunDateRange(ctx, from, to)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "获取趋势指标失败")
	}
	excluded, err := service.CountUnconfirmedByRunDateRange(ctx, from, to)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "统计未复核指标失败")
	}
	return &v1.GetSpotReportTrendRes{
		From:            from,
		To:              to,
		Grain:           string(grain),
		Points:          logic.ToTrendPoints(logic.BuildTrend(models, grain)),
		ExcludedPending: excluded,
	}, nil
}

func (c *ControllerV1) GetGdMetric(ctx context.Context, req *v1.GetGdMetricReq) (*v1.GetGdMetricRes, error) {
	if req.Date != "" && !datePattern.MatchString(req.Date) {
		return nil, gerror.NewCode(gcode.CodeInvalidParameter, "运行日格式无效")
	}
	for _, d := range []string{req.From, req.To} {
		if d != "" && !datePattern.MatchString(d) {
			return nil, gerror.NewCode(gcode.CodeInvalidParameter, "日期格式无效")
		}
	}
	// 这个快照接口读的是已停更的第三方通道：日频一律走官方（/api/spot-market/board），
	// 只有显式 allowFrozen 才返回冻结的历史演示数据。
	snap, err := gdmetric.GetSnapshot(ctx, req.From, req.To, req.Date)
	if gdmetric.IsChannelRetired(err) && req.AllowFrozen {
		snap, err = gdmetric.GetFrozenSnapshot(ctx, req.From, req.To, req.Date)
	}
	if err != nil {
		if gdmetric.IsChannelRetired(err) {
			return nil, gerror.NewCode(gcode.CodeInternalError, err.Error())
		}
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "加载电网日指标失败")
	}
	return &v1.GetGdMetricRes{Snapshot: snap}, nil
}
