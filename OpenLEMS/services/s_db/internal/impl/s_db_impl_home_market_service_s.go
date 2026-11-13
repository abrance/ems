package impl

import (
	"context"
	"s_db/s_db_basic"
	"s_db/s_db_model"
	"sync"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/pkg/errors"
)

type sHomeMarketServiceImpl struct{}

var (
	homeMarketServiceInstance s_db_basic.IHomeMarketService
	homeMarketServiceOnce     sync.Once
)

func GetHomeMarketService() s_db_basic.IHomeMarketService {
	homeMarketServiceOnce.Do(func() {
		homeMarketServiceInstance = &sHomeMarketServiceImpl{}
	})
	return homeMarketServiceInstance
}

// UpsertHome24h 覆盖写某天的 24 点。
//
// 用 ON CONFLICT 而不是先删后插：源站某天偶尔只返回部分小时（重试中途断掉），
// 先删后插会把上一轮已采到的小时也删掉，页面就从「24 点」变成「17 点」。
// 覆盖写只更新回来的那几行，缺的保持原样。
func (s *sHomeMarketServiceImpl) UpsertHome24h(ctx context.Context, rows []*s_db_model.SHome24hModel) (int, error) {
	n := 0
	for _, row := range rows {
		if row == nil || row.PriceDate == "" {
			continue
		}
		_, err := g.DB().Exec(ctx, `
			INSERT INTO gd_home_24h (price_date, hour, power_mw, ahead_price, realtime_price, source, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
			ON CONFLICT(price_date, hour) DO UPDATE SET
				power_mw = excluded.power_mw,
				ahead_price = excluded.ahead_price,
				realtime_price = excluded.realtime_price,
				source = excluded.source,
				updated_at = datetime('now')
		`, row.PriceDate, row.Hour, row.PowerMw, row.AheadPrice, row.RealtimePrice, row.Source)
		if err != nil {
			return n, errors.Wrap(err, "写入 24 点分时失败")
		}
		n++
	}
	return n, nil
}

func (s *sHomeMarketServiceImpl) ListHome24h(ctx context.Context, date string) ([]*s_db_model.SHome24hModel, error) {
	list := make([]*s_db_model.SHome24hModel, 0, 24)
	err := g.Model(s_db_model.TableHome24h).Ctx(ctx).
		Where(s_db_model.FieldHome24hPriceDate, date).
		OrderAsc(s_db_model.FieldHome24hHour).
		Scan(&list)
	if err != nil {
		return nil, errors.Wrap(err, "查询 24 点分时失败")
	}
	return list, nil
}

func (s *sHomeMarketServiceImpl) LatestHome24hDate(ctx context.Context) (string, error) {
	// 单字段查询扫进 []string 会被 gf 序列化成整行 JSON（见 s_db_impl_price_forecast_service_s.go 的坑），
	// 这里按行结构体接再取字段。
	var rows []struct {
		PriceDate string `orm:"price_date"`
	}
	err := g.Model(s_db_model.TableHome24h).Ctx(ctx).
		Fields(s_db_model.FieldHome24hPriceDate).
		OrderDesc(s_db_model.FieldHome24hPriceDate).
		Limit(1).
		Scan(&rows)
	if err != nil {
		return "", errors.Wrap(err, "查询 24 点分时最新日失败")
	}
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].PriceDate, nil
}

func (s *sHomeMarketServiceImpl) UpsertNodePrice(ctx context.Context, rows []*s_db_model.SNodePriceModel) (int, error) {
	n := 0
	for _, row := range rows {
		if row == nil || row.RunDate == "" || row.City == "" {
			continue
		}
		_, err := g.DB().Exec(ctx, `
			INSERT INTO gd_node_price (run_date, city, period_type, price, source, updated_at)
			VALUES (?, ?, ?, ?, ?, datetime('now'))
			ON CONFLICT(run_date, city, period_type) DO UPDATE SET
				price = excluded.price,
				source = excluded.source,
				updated_at = datetime('now')
		`, row.RunDate, row.City, row.PeriodType, row.Price, row.Source)
		if err != nil {
			return n, errors.Wrap(err, "写入地市节点价失败")
		}
		n++
	}
	return n, nil
}

func (s *sHomeMarketServiceImpl) ListNodePrice(ctx context.Context, date string) ([]*s_db_model.SNodePriceModel, error) {
	list := make([]*s_db_model.SNodePriceModel, 0, 42)
	err := g.Model(s_db_model.TableNodePrice).Ctx(ctx).
		Where(s_db_model.FieldNodePriceRunDate, date).
		OrderAsc(s_db_model.FieldNodePricePeriodType).
		OrderAsc(s_db_model.FieldNodePriceCity).
		Scan(&list)
	if err != nil {
		return nil, errors.Wrap(err, "查询地市节点价失败")
	}
	return list, nil
}

func (s *sHomeMarketServiceImpl) LatestNodePriceDate(ctx context.Context) (string, error) {
	var rows []struct {
		RunDate string `orm:"run_date"`
	}
	err := g.Model(s_db_model.TableNodePrice).Ctx(ctx).
		Fields(s_db_model.FieldNodePriceRunDate).
		OrderDesc(s_db_model.FieldNodePriceRunDate).
		Limit(1).
		Scan(&rows)
	if err != nil {
		return "", errors.Wrap(err, "查询地市节点价最新日失败")
	}
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].RunDate, nil
}
