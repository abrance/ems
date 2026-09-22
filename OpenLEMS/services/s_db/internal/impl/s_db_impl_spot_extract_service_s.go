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

// ReplaceMetrics 全量替换：同一日报重复提取时不会留下旧指标
func (s *sSpotExtractServiceImpl) ReplaceMetrics(ctx context.Context, reportDate string, metrics []*s_db_model.SSpotMetricModel) error {
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		if _, err := tx.Model(s_db_model.TableSpotMetric).Ctx(ctx).
			Where(s_db_model.FieldSpotMetricReportDate, reportDate).Delete(); err != nil {
			return err
		}
		if len(metrics) == 0 {
			return nil
		}
		rows := make([]g.Map, 0, len(metrics))
		now := gtime.Now()
		for _, m := range metrics {
			rows = append(rows, g.Map{
				s_db_model.FieldSpotMetricReportDate:    m.ReportDate,
				s_db_model.FieldSpotMetricRunDate:       m.RunDate,
				s_db_model.FieldSpotMetricDataset:       m.Dataset,
				s_db_model.FieldSpotMetricScope:         m.Scope,
				s_db_model.FieldSpotMetricMetric:        m.Metric,
				s_db_model.FieldSpotMetricValue:         m.Value,
				s_db_model.FieldSpotMetricUnit:          m.Unit,
				s_db_model.FieldSpotMetricAtTime:        m.AtTime,
				s_db_model.FieldSpotMetricMomPct:        m.MomPct,
				s_db_model.FieldSpotMetricSharePct:      m.SharePct,
				s_db_model.FieldSpotMetricSource:        m.Source,
				s_db_model.FieldSpotMetricConf:          m.Conf,
				s_db_model.FieldSpotMetricExtractorVers: m.ExtractorVersion,
				"created_at":                            now,
				"updated_at":                            now,
			})
		}
		_, err := tx.Model(s_db_model.TableSpotMetric).Ctx(ctx).Data(rows).Insert()
		return err
	})
	if err != nil {
		return errors.Wrap(err, "保存现货指标失败")
	}
	return nil
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
