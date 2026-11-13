package impl

import (
	"context"
	"s_db/s_db_basic"
	"s_db/s_db_model"
	"sync"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
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

// RecordFetchRun 记一次采集尝试。
//
// 先读旧行再覆盖写（而不是一条 ON CONFLICT 里做 CASE 算术）：连续失败/连续空
// 这两个计数要读旧值，写成 SQL 表达式既难读又只有一个调用方会受益。
// shortcut: 读-改-写不是原子的，同一 (target_date,dataset) 并发写会丢一次计数；
// 采集是单协程串行调用，真并行采集时改成 ON CONFLICT 里的 CASE 算术。
func (s *sHomeMarketServiceImpl) RecordFetchRun(ctx context.Context, run *s_db_model.SFetchRunModel) (*s_db_model.SFetchRunModel, error) {
	if run == nil || run.TargetDate == "" || run.Dataset == "" {
		return nil, errors.New("采集运行记录缺少 target_date/dataset")
	}
	prev, err := s.fetchRun(ctx, run.TargetDate, run.Dataset)
	if err != nil {
		return nil, err
	}

	run.AttemptCount = 1
	run.ConsecutiveFailures = 0
	run.ConsecutiveEmpties = 0
	switch run.Status {
	case s_db_model.FetchStatusFailed:
		run.ConsecutiveFailures = 1
	case s_db_model.FetchStatusEmpty:
		run.ConsecutiveEmpties = 1
	}
	if prev != nil {
		run.AttemptCount = prev.AttemptCount + 1
		if run.Status == s_db_model.FetchStatusFailed {
			run.ConsecutiveFailures = prev.ConsecutiveFailures + 1
		}
		if run.Status == s_db_model.FetchStatusEmpty {
			run.ConsecutiveEmpties = prev.ConsecutiveEmpties + 1
		}
		// 失败/为空不抹掉上一次成功时间：页面要看的是「上次有数是什么时候」。
		run.LastOkAt = prev.LastOkAt
	}
	if run.Status == s_db_model.FetchStatusOK {
		run.LastOkAt = gtime.Now()
	}

	_, err = g.DB().Exec(ctx, `
		INSERT INTO gd_home_market_fetch_run
			(target_date, dataset, status, error_kind, rows_written, rows_expected,
			 attempt_count, consecutive_failures, consecutive_empties, last_error,
			 first_seen_at, last_ok_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), ?, datetime('now'))
		ON CONFLICT(target_date, dataset) DO UPDATE SET
			status = excluded.status,
			error_kind = excluded.error_kind,
			rows_written = excluded.rows_written,
			rows_expected = excluded.rows_expected,
			attempt_count = excluded.attempt_count,
			consecutive_failures = excluded.consecutive_failures,
			consecutive_empties = excluded.consecutive_empties,
			last_error = excluded.last_error,
			last_ok_at = excluded.last_ok_at,
			updated_at = datetime('now')
	`, run.TargetDate, run.Dataset, run.Status, run.ErrorKind, run.RowsWritten, run.RowsExpected,
		run.AttemptCount, run.ConsecutiveFailures, run.ConsecutiveEmpties, run.LastError,
		nullableTime(run.LastOkAt))
	if err != nil {
		return nil, errors.Wrap(err, "写入采集运行记录失败")
	}
	return s.fetchRun(ctx, run.TargetDate, run.Dataset)
}

func (s *sHomeMarketServiceImpl) LatestFetchRun(ctx context.Context, dataset string) (*s_db_model.SFetchRunModel, error) {
	// 按 id 兜底排序：同一轮里 14 个日期是同一秒写进去的，updated_at 会撞。
	var rows []*s_db_model.SFetchRunModel
	err := g.Model(s_db_model.TableFetchRun).Ctx(ctx).
		Where("dataset", dataset).
		OrderDesc("id").
		Limit(1).
		Scan(&rows)
	if err != nil {
		return nil, errors.Wrap(err, "查询最近一次采集记录失败")
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (s *sHomeMarketServiceImpl) LatestOKFetchRun(ctx context.Context, dataset string) (*s_db_model.SFetchRunModel, error) {
	var rows []*s_db_model.SFetchRunModel
	err := g.Model(s_db_model.TableFetchRun).Ctx(ctx).
		Where("dataset", dataset).
		Where("status", s_db_model.FetchStatusOK).
		OrderDesc("target_date").
		OrderDesc("id").
		Limit(1).
		Scan(&rows)
	if err != nil {
		return nil, errors.Wrap(err, "查询最近一次成功采集记录失败")
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (s *sHomeMarketServiceImpl) fetchRun(ctx context.Context, targetDate, dataset string) (*s_db_model.SFetchRunModel, error) {
	var rows []*s_db_model.SFetchRunModel
	err := g.Model(s_db_model.TableFetchRun).Ctx(ctx).
		Where("target_date", targetDate).
		Where("dataset", dataset).
		Limit(1).
		Scan(&rows)
	if err != nil {
		return nil, errors.Wrap(err, "查询采集运行记录失败")
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// nullableTime 让 nil 走进 sql 的 NULL（gtime 零值会被写成 0001-01-01，破坏「从没成功过」的语义）。
func nullableTime(t *gtime.Time) interface{} {
	if t == nil || t.IsZero() {
		return nil
	}
	return t
}
