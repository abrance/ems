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
	// 同一天同一算法对同一目标时刻重算只更新，不堆行。
	for _, row := range rows {
		if row == nil || row.TargetTime == "" {
			continue
		}
		algo := row.Algo
		if algo == "" {
			algo = s_db_model.ForecastAlgoTimesFM
		}
		_, err := g.DB().Exec(ctx, `
			INSERT INTO gd_price_forecast
				(generated_on, market, grain, algo, target_time, point, q10, q50, q90, model, unit, source, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			ON CONFLICT(generated_on, market, grain, algo, target_time) DO UPDATE SET
				point = excluded.point, q10 = excluded.q10, q50 = excluded.q50, q90 = excluded.q90,
				model = excluded.model, unit = excluded.unit, source = excluded.source,
				updated_at = CURRENT_TIMESTAMP
		`, row.GeneratedOn, row.Market, row.Grain, algo, row.TargetTime,
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
	// 只返回「至少有一条可用于复盘」的生成日。判据与复盘页剔除回填行的那条
	// 完全同源（见 logic/forecast 的 IsBackfilledForecast：generated_on > target 的日期部分
	// 就是事后补算），所以这里能配对 ⟺ target_time 的日期部分 >= generated_on。
	//
	// 为什么必须过滤：CSV 时代留下 30 行历史回填（6 个生成日，target 全早于
	// generated_on），DISTINCT 会把它们当「留痕自 2026-10-07 起积累」显示给用户，
	// 于是空态文案算出「首个可配对交易日约在 2026-10-10」——一个多月前的日子。
	//
	// intraday 同日不算回填（复盘页保留当天后续时段的点），用 >= 正好覆盖，不需要按 grain 分支。
	// 两边都截前 10 位：generated_on 可能有带时间的脏值，不截的话同一天会因为
	// '2026-11-11' >= '2026-11-11 08:55:15' 为假而被误判成回填。
	where := "1=1"
	args := make([]any, 0, 2)
	if strings.TrimSpace(market) != "" {
		where += " AND market = ?"
		args = append(args, market)
	}
	if strings.TrimSpace(grain) != "" {
		where += " AND grain = ?"
		args = append(args, grain)
	}
	// 注意：不能直接 `Scan(&[]string)` —— 单字段查询扫进 []string 时，gf 会把**整行**
	// 序列化成 JSON 字符串塞进数组（线上实测拿到的是 `{"generated_on":"2026-10-07"}`，
	// 页面直接把这段字符串显示给用户了）。按行结构体接，再自己取字段。
	var rows []struct {
		GeneratedOn string `orm:"generated_on"`
	}
	if err := g.DB().GetScan(ctx, &rows, `
		SELECT generated_on
		  FROM `+s_db_model.TablePriceForecast+`
		 WHERE `+where+`
		 GROUP BY generated_on
		HAVING SUM(CASE WHEN substr(target_time, 1, 10) >= substr(generated_on, 1, 10) THEN 1 ELSE 0 END) > 0
		 ORDER BY generated_on ASC`, args...); err != nil {
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
