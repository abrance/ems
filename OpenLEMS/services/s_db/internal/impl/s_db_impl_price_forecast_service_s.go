package impl

import (
	"context"
	"s_db/s_db_basic"
	"s_db/s_db_model"
	"strings"
	"sync"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/pkg/errors"
)

type sPriceForecastServiceImpl struct{}

var (
	priceForecastServiceInstance s_db_basic.IPriceForecastService
	priceForecastServiceOnce     sync.Once
)

func GetPriceForecastService() s_db_basic.IPriceForecastService {
	priceForecastServiceOnce.Do(func() {
		priceForecastServiceInstance = &sPriceForecastServiceImpl{}
	})
	return priceForecastServiceInstance
}

func (s *sPriceForecastServiceImpl) UpsertBatch(ctx context.Context, rows []*s_db_model.SPriceForecastModel) error {
	if len(rows) == 0 {
		return nil
	}
	// 唯一键在 SQL 里声明（见 s_db_init_f.go），这里用 ON CONFLICT 覆盖：
	// 同一天对同一目标时刻重算只更新，不堆行。
	for _, row := range rows {
		if row == nil || row.TargetTime == "" {
			continue
		}
		_, err := g.DB().Exec(ctx, `
			INSERT INTO gd_price_forecast
				(generated_on, market, grain, target_time, point, q10, q50, q90, model, unit, source, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
			ON CONFLICT(generated_on, market, grain, target_time) DO UPDATE SET
				point = excluded.point, q10 = excluded.q10, q50 = excluded.q50, q90 = excluded.q90,
				model = excluded.model, unit = excluded.unit, source = excluded.source,
				updated_at = datetime('now')
		`, row.GeneratedOn, row.Market, row.Grain, row.TargetTime,
			row.Point, row.Q10, row.Q50, row.Q90, row.Model, row.Unit, row.Source)
		if err != nil {
			return errors.Wrap(err, "写入电价预测留痕失败")
		}
	}
	return nil
}

func (s *sPriceForecastServiceImpl) ListByTargetRange(
	ctx context.Context, from, to, market, grain string,
) ([]*s_db_model.SPriceForecastModel, error) {
	m := g.Model(s_db_model.TablePriceForecast).Ctx(ctx)
	if strings.TrimSpace(market) != "" {
		m = m.Where(s_db_model.FieldPriceForecastMarket, market)
	}
	if strings.TrimSpace(grain) != "" {
		m = m.Where(s_db_model.FieldPriceForecastGrain, grain)
	}
	if strings.TrimSpace(from) != "" {
		m = m.WhereGTE(s_db_model.FieldPriceForecastTargetTime, from)
	}
	if strings.TrimSpace(to) != "" {
		m = m.WhereLTE(s_db_model.FieldPriceForecastTargetTime, to)
	}
	var list []*s_db_model.SPriceForecastModel
	if err := m.OrderAsc(s_db_model.FieldPriceForecastTargetTime).Scan(&list); err != nil {
		return nil, errors.Wrap(err, "查询电价预测留痕失败")
	}
	return list, nil
}

func (s *sPriceForecastServiceImpl) GeneratedDates(
	ctx context.Context, market, grain string,
) ([]string, error) {
	m := g.Model(s_db_model.TablePriceForecast).Ctx(ctx).Fields(s_db_model.FieldPriceForecastGeneratedOn).Distinct()
	if strings.TrimSpace(market) != "" {
		m = m.Where(s_db_model.FieldPriceForecastMarket, market)
	}
	if strings.TrimSpace(grain) != "" {
		m = m.Where(s_db_model.FieldPriceForecastGrain, grain)
	}
	// 注意：不能直接 `Scan(&[]string)` —— 单字段查询扫进 []string 时，gf 会把**整行**
	// 序列化成 JSON 字符串塞进数组（线上实测拿到的是 `{"generated_on":"2026-10-07"}`，
	// 页面直接把这段字符串显示给用户了）。按行结构体接，再自己取字段。
	var rows []struct {
		GeneratedOn string `orm:"generated_on"`
	}
	if err := m.OrderAsc(s_db_model.FieldPriceForecastGeneratedOn).Scan(&rows); err != nil {
		return nil, errors.Wrap(err, "查询预测留痕生成日失败")
	}
	dates := make([]string, 0, len(rows))
	for _, row := range rows {
		date := strings.TrimSpace(row.GeneratedOn)
		if date == "" {
			continue
		}
		// 只管日期部分（列里存的是 YYYY-MM-DD，防御性截一下时间）
		if len(date) > 10 {
			date = date[:10]
		}
		dates = append(dates, date)
	}
	return dates, nil
}
