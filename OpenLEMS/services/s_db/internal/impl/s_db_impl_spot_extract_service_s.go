package impl

import (
	"context"
	"database/sql"
	"s_db/s_db_basic"
	"s_db/s_db_model"
	"sync"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
	"github.com/pkg/errors"
)

type sSpotExtractServiceImpl struct{}

var (
	spotExtractServiceInstance s_db_basic.ISpotExtractService
	spotExtractServiceOnce     sync.Once
)

func GetSpotExtractService() s_db_basic.ISpotExtractService {
	spotExtractServiceOnce.Do(func() {
		spotExtractServiceInstance = &sSpotExtractServiceImpl{}
	})
	return spotExtractServiceInstance
}

// ReplaceMetrics 全量替换：同一日报重复提取时不会留下旧指标。
//
// 重跑语义为「以 OCR 为准」：人工修订行会被覆盖，但覆盖前把它们写入审计表，
// 并作为返回值交给调用方提示（避免人工判断无声消失）。
func (s *sSpotExtractServiceImpl) ReplaceMetrics(ctx context.Context, reportDate string, metrics []*s_db_model.SSpotMetricModel) ([]s_db_basic.SpotMetricOverwrite, error) {
	var overwritten []s_db_basic.SpotMetricOverwrite
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		overwritten = overwritten[:0]
		newValues := make(map[string]*float64, len(metrics))
		for _, m := range metrics {
			v := m.Value
			newValues[metricKey(m.ReportDate, m.RunDate, m.Dataset, m.Scope, m.Metric)] = &v
		}

		var manual []*s_db_model.SSpotMetricModel
		if err := tx.Model(s_db_model.TableSpotMetric).Ctx(ctx).
			Where(s_db_model.FieldSpotMetricReportDate, reportDate).
			Where(s_db_model.FieldSpotMetricOrigin, "manual").
			Scan(&manual); err != nil {
			return err
		}
		now := gtime.Now()
		for _, m := range manual {
			row := g.Map{
				"report_date": m.ReportDate, "run_date": m.RunDate, "dataset": m.Dataset,
				"scope": m.Scope, "metric": m.Metric, "old_value": m.Value,
				"new_value":  newValues[metricKey(m.ReportDate, m.RunDate, m.Dataset, m.Scope, m.Metric)],
				"old_origin": "manual", "operation": s_db_model.SpotMetricAuditOverwrite,
				"operator": "extract", "created_at": now,
			}
			if _, err := tx.Model(s_db_model.TableSpotMetricAudit).Ctx(ctx).Data(row).Insert(); err != nil {
				return err
			}
			overwritten = append(overwritten, s_db_basic.SpotMetricOverwrite{
				Key: s_db_basic.SpotMetricKey{
					ReportDate: m.ReportDate, RunDate: m.RunDate, Dataset: m.Dataset,
					Scope: m.Scope, Metric: m.Metric, SourceLabel: m.Source,
				},
				OldValue: m.Value,
				NewValue: newValues[metricKey(m.ReportDate, m.RunDate, m.Dataset, m.Scope, m.Metric)],
			})
		}

		if _, err := tx.Model(s_db_model.TableSpotMetric).Ctx(ctx).
			Where(s_db_model.FieldSpotMetricReportDate, reportDate).Delete(); err != nil {
			return err
		}
		if len(metrics) == 0 {
			return nil
		}
		rows := make([]g.Map, 0, len(metrics))
		for _, m := range metrics {
			if m.Origin == "" {
				m.Origin = "ocr"
			}
			rows = append(rows, g.Map{
				s_db_model.FieldSpotMetricReportDate:    m.ReportDate,
				s_db_model.FieldSpotMetricRunDate:       m.RunDate,
				s_db_model.FieldSpotMetricDataset:       m.Dataset,
				s_db_model.FieldSpotMetricScope:         m.Scope,
				s_db_model.FieldSpotMetricMetric:        m.Metric,
				s_db_model.FieldSpotMetricValue:         m.Value,
				s_db_model.FieldSpotMetricUnit:          m.Unit,
				s_db_model.FieldSpotMetricAtTime:        m.AtTime,
				"text_value":                            m.TextValue,
				s_db_model.FieldSpotMetricMomPct:        m.MomPct,
				s_db_model.FieldSpotMetricSharePct:      m.SharePct,
				s_db_model.FieldSpotMetricSource:        m.Source,
				s_db_model.FieldSpotMetricConf:          m.Conf,
				s_db_model.FieldSpotMetricExtractorVers: m.ExtractorVersion,
				s_db_model.FieldSpotMetricOrigin:        m.Origin,
				"created_at":                            now,
				"updated_at":                            now,
			})
		}
		_, err := tx.Model(s_db_model.TableSpotMetric).Ctx(ctx).Data(rows).Insert()
		return err
	})
	if err != nil {
		return nil, errors.Wrap(err, "保存现货指标失败")
	}
	return overwritten, nil
}

func metricKey(reportDate, runDate, dataset, scope, metric string) string {
	return reportDate + "|" + runDate + "|" + dataset + "|" + scope + "|" + metric
}

// UpdateMetricValue 人工修订单个指标：更新值并把 origin 置为 manual，同时写审计
func (s *sSpotExtractServiceImpl) UpdateMetricValue(ctx context.Context, key s_db_basic.SpotMetricKey, value float64, operator string) (*s_db_model.SSpotMetricModel, error) {
	model := &s_db_model.SSpotMetricModel{}
	if err := g.Model(s_db_model.TableSpotMetric).Ctx(ctx).
		Where(s_db_model.FieldSpotMetricReportDate, key.ReportDate).
		Where(s_db_model.FieldSpotMetricRunDate, key.RunDate).
		Where(s_db_model.FieldSpotMetricDataset, key.Dataset).
		Where(s_db_model.FieldSpotMetricScope, key.Scope).
		Where(s_db_model.FieldSpotMetricMetric, key.Metric).
		Scan(model); err != nil {
		return nil, errors.Wrap(err, "查询待修订指标失败")
	}
	if model.Id == 0 {
		return nil, errors.New("未找到该指标，请先执行解析")
	}
	oldValue := model.Value
	now := gtime.Now()
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		if _, err := tx.Model(s_db_model.TableSpotMetric).Ctx(ctx).
			Where("id", model.Id).
			Data(g.Map{
				s_db_model.FieldSpotMetricValue:    value,
				s_db_model.FieldSpotMetricOrigin:   "manual",
				s_db_model.FieldSpotMetricEditedBy: operator,
				s_db_model.FieldSpotMetricEditedAt: now,
				"updated_at":                       now,
			}).Update(); err != nil {
			return err
		}
		_, err := tx.Model(s_db_model.TableSpotMetricAudit).Ctx(ctx).Data(g.Map{
			"report_date": key.ReportDate, "run_date": key.RunDate, "dataset": key.Dataset,
			"scope": key.Scope, "metric": key.Metric,
			"old_value": oldValue, "new_value": value, "old_origin": model.Origin,
			"operation": s_db_model.SpotMetricAuditEdit, "operator": operator, "created_at": now,
		}).Insert()
		return err
	})
	if err != nil {
		return nil, errors.Wrap(err, "保存人工修订失败")
	}
	model.Value = value
	model.Origin = "manual"
	model.EditedBy = operator
	model.EditedAt = now
	return model, nil
}

// ListMetricAudit 指标变更审计（人工修订 + 被解析覆盖）
func (s *sSpotExtractServiceImpl) ListMetricAudit(ctx context.Context, reportDate string, limit int) ([]*s_db_model.SSpotMetricAuditModel, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var list []*s_db_model.SSpotMetricAuditModel
	err := g.Model(s_db_model.TableSpotMetricAudit).Ctx(ctx).
		Where(s_db_model.FieldSpotMetricAuditReport, reportDate).
		OrderDesc("id").
		Limit(limit).
		Scan(&list)
	if err != nil {
		return nil, errors.Wrap(err, "查询指标审计失败")
	}
	return list, nil
}

func (s *sSpotExtractServiceImpl) ListMetrics(ctx context.Context, reportDate string) ([]*s_db_model.SSpotMetricModel, error) {
	var list []*s_db_model.SSpotMetricModel
	err := g.Model(s_db_model.TableSpotMetric).Ctx(ctx).
		Where(s_db_model.FieldSpotMetricReportDate, reportDate).
		OrderAsc(s_db_model.FieldSpotMetricRunDate).
		OrderAsc(s_db_model.FieldSpotMetricDataset).
		OrderAsc(s_db_model.FieldSpotMetricScope).
		Scan(&list)
	if err != nil {
		return nil, errors.Wrap(err, "查询现货指标失败")
	}
	return list, nil
}

func (s *sSpotExtractServiceImpl) ListMetricsByRunDate(ctx context.Context, runDate string) ([]*s_db_model.SSpotMetricModel, error) {
	var list []*s_db_model.SSpotMetricModel
	err := g.Model(s_db_model.TableSpotMetric).Ctx(ctx).
		Where(s_db_model.FieldSpotMetricRunDate, runDate).
		OrderAsc(s_db_model.FieldSpotMetricDataset).
		OrderAsc(s_db_model.FieldSpotMetricScope).
		Scan(&list)
	if err != nil {
		return nil, errors.Wrap(err, "按运行日查询现货指标失败")
	}
	return list, nil
}

func (s *sSpotExtractServiceImpl) GetRun(ctx context.Context, reportDate string) (*s_db_model.SSpotExtractRunModel, error) {
	model := &s_db_model.SSpotExtractRunModel{}
	err := g.Model(s_db_model.TableSpotExtractRun).Ctx(ctx).
		Where(s_db_model.FieldSpotExtractReportDate, reportDate).
		Scan(model)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "查询提取记录失败")
	}
	if model.Id == 0 && model.ReportDate == "" {
		return nil, nil
	}
	return model, nil
}

func (s *sSpotExtractServiceImpl) UpsertRun(ctx context.Context, model *s_db_model.SSpotExtractRunModel) error {
	existing, err := s.GetRun(ctx, model.ReportDate)
	if err != nil {
		return err
	}
	now := gtime.Now()
	model.UpdatedAt = now
	if existing == nil {
		model.CreatedAt = now
		_, err := g.Model(s_db_model.TableSpotExtractRun).Ctx(ctx).FieldsEx("id").Insert(model)
		return errors.Wrap(err, "创建提取记录失败")
	}
	model.Id = existing.Id
	_, err = g.Model(s_db_model.TableSpotExtractRun).Ctx(ctx).
		Where("id", existing.Id).
		FieldsEx("id", "created_at").
		Update(model)
	return errors.Wrap(err, "覆盖提取记录失败")
}
