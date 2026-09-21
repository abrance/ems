package impl

import (
	"context"
	"database/sql"
	"s_db/s_db_basic"
	"s_db/s_db_model"
	"sync"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/pkg/errors"
)

type sSpotReportServiceImpl struct{}

var (
	spotReportServiceInstance s_db_basic.ISpotReportService
	spotReportServiceOnce     sync.Once
)

func GetSpotReportService() s_db_basic.ISpotReportService {
	spotReportServiceOnce.Do(func() {
		spotReportServiceInstance = &sSpotReportServiceImpl{}
	})
	return spotReportServiceInstance
}

func (s *sSpotReportServiceImpl) List(ctx context.Context) ([]*s_db_model.SSpotReportModel, error) {
	var list []*s_db_model.SSpotReportModel
	err := g.Model(s_db_model.TableSpotReport).Ctx(ctx).
		OrderDesc(s_db_model.FieldSpotReportRunDate).
		Scan(&list)
	if err != nil {
		return nil, errors.Wrap(err, "查询现货日报列表失败")
	}
	return list, nil
}

func (s *sSpotReportServiceImpl) GetByDate(ctx context.Context, runDate string) (*s_db_model.SSpotReportModel, error) {
	model := &s_db_model.SSpotReportModel{}
	err := g.Model(s_db_model.TableSpotReport).Ctx(ctx).
		Where(s_db_model.FieldSpotReportRunDate, runDate).
		Scan(model)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "查询现货日报失败")
	}
	if model.Id == 0 && model.RunDate == "" {
		return nil, nil
	}
	return model, nil
}

func (s *sSpotReportServiceImpl) GetLatest(ctx context.Context) (*s_db_model.SSpotReportModel, error) {
	model := &s_db_model.SSpotReportModel{}
	err := g.Model(s_db_model.TableSpotReport).Ctx(ctx).
		OrderDesc(s_db_model.FieldSpotReportRunDate).
		Limit(1).
		Scan(model)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "查询最近现货日报失败")
	}
	if model.Id == 0 && model.RunDate == "" {
		return nil, nil
	}
	return model, nil
}

func (s *sSpotReportServiceImpl) Upsert(ctx context.Context, model *s_db_model.SSpotReportModel) error {
	existing, err := s.GetByDate(ctx, model.RunDate)
	if err != nil {
		return err
	}
	if existing == nil {
		return errors.Wrap(model.Create(ctx), "创建现货日报失败")
	}
	model.Id = existing.Id
	model.CreatedAt = existing.CreatedAt
	return errors.Wrap(model.Update(ctx), "覆盖现货日报失败")
}
