package spotreport

import (
	"context"
	"regexp"

	v1 "application/api/spotreport/v1"
	logic "application/internal/logic/spotreport"
	"s_db"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func (c *ControllerV1) ListSpotReport(ctx context.Context, req *v1.ListSpotReportReq) (*v1.ListSpotReportRes, error) {
	models, err := s_db.GetSpotReportService().List(ctx)
	if err != nil {
		return nil, gerror.WrapCode(gcode.CodeInternalError, err, "获取现货日报列表失败")
	}
	list := make([]*v1.SpotReport, 0, len(models))
	for _, model := range models {
		list = append(list, logic.ToDTO(model))
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
	res, err := logic.ExtractReport(ctx, req.Date)
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
