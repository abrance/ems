package impl

import (
	"context"
	"sync"

	"s_db/s_db_basic"
	"s_db/s_db_model"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/pkg/errors"
)

type sReportDailyServiceImpl struct{}

var (
	reportDailyServiceInstance s_db_basic.IReportDailyService
	reportDailyServiceOnce     sync.Once
)

func GetReportDailyService() s_db_basic.IReportDailyService {
	reportDailyServiceOnce.Do(func() {
		reportDailyServiceInstance = &sReportDailyServiceImpl{}
	})
	return reportDailyServiceInstance
}

// UpsertBatch 按唯一键 (run_date, dataset, scope, metric) 覆盖写。
//
// force=false 时有两层保留：
//  1. origin=manual 的行：核对页上的日报原值允许人工修订，重跑解析不该把人改过的
//     数悄悄换回 OCR 值（与旧链路 ReplaceMetrics 的语义一致），计入 Preserved。
//  2. 「报告日最接近运行日」：同一运行日会被多份日报覆盖（表1(二) 当日 / 表2(一) T-2），
//     只有新的报告日距离**严格更小**时才覆盖，否则保留已有行计入 SkippedFarther。
//     距离相等时保留已有行（先到者胜，与 BackfillFromSpotMetric 的 INSERT OR IGNORE 同口径）。
//     少了这条，活链路会被后写进来的远日报覆盖，而 compare 仍对外宣称「按最接近只留一行」。
func (s *sReportDailyServiceImpl) UpsertBatch(ctx context.Context, rows []*s_db_model.SReportDailyModel, force bool) (s_db_basic.ReportDailyWriteResult, error) {
	res := s_db_basic.ReportDailyWriteResult{}
	for _, row := range rows {
		if row == nil || row.RunDate == "" || row.Dataset == "" || row.Scope == "" || row.Metric == "" {
			continue
		}
		if !force {
			// 单字段扫进 *string 会被 gf 拒绝（in valid parameter type），按行结构体接。
			// 距离算式与 BackfillFromSpotMetric 的 ORDER BY 完全一致，且都交给 sqlite 算，
			// 避免 Go 侧再实现一份日期差。
			var existing []struct {
				Origin  string  `orm:"origin"`
				Dist    float64 `orm:"dist"`
				NewDist float64 `orm:"new_dist"`
			}
			err := g.DB().GetScan(ctx, &existing, `
				SELECT origin,
				       ABS(julianday(report_date) - julianday(?)) AS dist,
				       ABS(julianday(?) - julianday(?)) AS new_dist
				FROM gd_report_daily
				WHERE run_date = ? AND dataset = ? AND scope = ? AND metric = ?`,
				row.RunDate, row.ReportDate, row.RunDate, row.RunDate, row.Dataset, row.Scope, row.Metric)
			if err != nil {
				return res, errors.Wrap(err, "查询日报原值已有行失败")
			}
			if len(existing) > 0 {
				if existing[0].Origin == s_db_model.ReportDailyOriginManual {
					res.Preserved++
					continue
				}
				if !(existing[0].NewDist < existing[0].Dist) {
					res.SkippedFarther++
					continue
				}
			}
		}
		_, err := g.DB().Exec(ctx, `
			INSERT INTO gd_report_daily
				(run_date, report_date, dataset, scope, metric, value, unit, source, origin,
				 extractor_version, report_title, collected_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
			ON CONFLICT(run_date, dataset, scope, metric) DO UPDATE SET
				report_date = excluded.report_date,
				value = excluded.value,
				unit = excluded.unit,
				source = excluded.source,
				origin = excluded.origin,
				extractor_version = excluded.extractor_version,
				report_title = excluded.report_title,
				collected_at = excluded.collected_at,
				updated_at = datetime('now')
		`, row.RunDate, row.ReportDate, row.Dataset, row.Scope, row.Metric, row.Value, row.Unit,
			row.Source, row.Origin, row.ExtractorVersion, row.ReportTitle, row.CollectedAt)
		if err != nil {
			return res, errors.Wrap(err, "写入日报原值失败")
		}
		res.Written++
	}
	return res, nil
}

func (s *sReportDailyServiceImpl) ListByRunDateRange(ctx context.Context, from, to string) ([]*s_db_model.SReportDailyModel, error) {
	list := make([]*s_db_model.SReportDailyModel, 0, 64)
	err := g.Model(s_db_model.TableReportDaily).Ctx(ctx).
		WhereGTE(s_db_model.FieldReportDailyRunDate, from).
		WhereLTE(s_db_model.FieldReportDailyRunDate, to).
		OrderAsc(s_db_model.FieldReportDailyRunDate).
		OrderAsc(s_db_model.FieldReportDailyDataset).
		OrderAsc(s_db_model.FieldReportDailyScope).
		OrderAsc(s_db_model.FieldReportDailyMetric).
		Scan(&list)
	if err != nil {
		return nil, errors.Wrap(err, "查询日报原值失败")
	}
	return list, nil
}

func (s *sReportDailyServiceImpl) LatestRunDate(ctx context.Context) (string, error) {
	// 单字段扫进 []string 会被 gf 序列化成整行 JSON（见 price_forecast service 的坑），按行结构体接。
	var rows []struct {
		RunDate string `orm:"run_date"`
	}
	err := g.Model(s_db_model.TableReportDaily).Ctx(ctx).
		Fields(s_db_model.FieldReportDailyRunDate).
		OrderDesc(s_db_model.FieldReportDailyRunDate).
		Limit(1).
		Scan(&rows)
	if err != nil {
		return "", errors.Wrap(err, "查询日报原值最新日失败")
	}
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].RunDate, nil
}

// BackfillFromSpotMetric 从 gd_spot_metric 回填历史日报原值（一次性、单向、幂等）。
//
// 只读 gd_spot_metric，不写它、不改 origin、不作废任何行。origin=void 的行不回填
// （已被官方取代，回填等于把废值搬到核对页左列）。
//
// 同一 run_date 可能由两份日报产出同一指标（表1(二) 当日 / 表2(一) T-2），按规范只留
// 「报告日最接近运行日」的那份：靠 SELECT 的 ORDER BY 保证最先插入的那行获胜，
// 后面的同键行被 INSERT OR IGNORE 丢弃，丢弃条数计入 Conflict。
func (s *sReportDailyServiceImpl) BackfillFromSpotMetric(ctx context.Context) (s_db_basic.ReportDailyBackfillResult, error) {
	res := s_db_basic.ReportDailyBackfillResult{}

	counts := []struct {
		Skipped  int `orm:"skipped"`
		Conflict int `orm:"conflict"`
	}{}
	// 口径与下面的 INSERT 完全一致，只是把「被忽略的行」数出来：按 (run_date,dataset,scope,metric)
	// 分组后，组内行数 -1 就是因「报告日最接近」规则被丢弃的冲突行。
	err := g.DB().GetScan(ctx, &counts, `
		SELECT
			COALESCE(SUM(CASE WHEN origin = 'void' THEN 1 ELSE 0 END), 0) AS skipped,
			COALESCE(SUM(cnt - 1), 0) AS conflict
		FROM (
			SELECT origin, COUNT(*) AS cnt
			FROM gd_spot_metric
			WHERE origin <> 'void'
			  AND ((dataset IN ('day_ahead_trade','realtime_trade')
			        AND scope IN ('发电侧','燃煤','燃气','新能源') AND metric = '平均电价')
			    OR (dataset = 'day_ahead_decl'
			        AND metric IN ('申报容量','煤机容量','气机容量','新能源容量','自主申报台数','默认申报台数')))
			GROUP BY run_date, dataset, scope, metric
		)
	`)
	if err != nil {
		return res, errors.Wrap(err, "统计回填冲突失败")
	}
	if len(counts) > 0 {
		res.Skipped = counts[0].Skipped
		res.Conflict = counts[0].Conflict
	}

	ret, err := g.DB().Exec(ctx, `
		INSERT OR IGNORE INTO gd_report_daily
			(run_date, report_date, dataset, scope, metric, value, unit, source, origin,
			 extractor_version, report_title, collected_at)
		SELECT m.run_date, m.report_date, m.dataset, m.scope, m.metric, m.value, m.unit, m.source,
		       CASE WHEN m.origin = 'manual' THEN 'manual' ELSE 'ocr' END,
		       m.extractor_version, COALESCE(r.title, ''), m.created_at
		FROM gd_spot_metric m
		LEFT JOIN gd_spot_report r ON r.run_date = m.report_date
		WHERE m.origin <> 'void'
		  AND ((m.dataset IN ('day_ahead_trade','realtime_trade')
		        AND m.scope IN ('发电侧','燃煤','燃气','新能源') AND m.metric = '平均电价')
		    OR (m.dataset = 'day_ahead_decl'
		        AND m.metric IN ('申报容量','煤机容量','气机容量','新能源容量','自主申报台数','默认申报台数')))
		ORDER BY m.run_date, m.dataset, m.scope, m.metric,
		         ABS(julianday(m.report_date) - julianday(m.run_date)), m.report_date
	`)
	if err != nil {
		return res, errors.Wrap(err, "回填日报原值失败")
	}
	if n, err := ret.RowsAffected(); err == nil {
		res.Inserted = int(n)
	}
	return res, nil
}

// ListOfficialDaily 读官方直采（origin=pm-api）的日频电价与申报均价。
//
// 只取核对页真正能对上的三个官方指标：日前/实时加权平均（与日报同义，算差值）、
// 日前申报均价（简单 vs 加权，只并排）。
// 官方没有分电源拆分与申报结构，那些行由核对页判 official_missing，不在这里补 0。
// 加权最高/最低价也不取：日报的最高/最低电价不进 gd_report_daily（只存分电源均价 + 申报结构），
// 左列没有值，单独列一列官方数字没有对照意义。
func (s *sReportDailyServiceImpl) LatestOfficialRunDate(ctx context.Context) (string, error) {
	// 口径与 ListOfficialDaily 一致：只认核对页真正能对上的三个指标，避免被其它历史行
	// （HHI、中长期月频等）把默认区间右端带跑。
	var rows []struct {
		RunDate string `orm:"run_date"`
	}
	err := g.DB().GetScan(ctx, &rows, `
		SELECT MAX(run_date) AS run_date
		FROM gd_spot_metric
		WHERE origin = 'pm-api'
		  AND (
		        (dataset IN ('day_ahead_trade','realtime_trade')
		         AND metric IN ('发电侧日前加权平均电价','发电侧实时加权平均电价'))
		     OR (dataset = 'day_ahead_trade' AND metric = '日前申报均价')
		      )`)
	if err != nil {
		return "", errors.Wrap(err, "查询官方日频指标最新运行日失败")
	}
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].RunDate, nil
}

func (s *sReportDailyServiceImpl) ListOfficialDaily(ctx context.Context, from, to string) ([]s_db_basic.ReportDailyOfficialRow, error) {
	rows := make([]s_db_basic.ReportDailyOfficialRow, 0, 256)
	err := g.DB().GetScan(ctx, &rows, `
		SELECT run_date, dataset, scope, metric, value, unit
		FROM gd_spot_metric
		WHERE origin = 'pm-api'
		  AND run_date >= ? AND run_date <= ?
		  AND (
		        (dataset IN ('day_ahead_trade','realtime_trade')
		         AND metric IN ('发电侧日前加权平均电价','发电侧实时加权平均电价'))
		     OR (dataset = 'day_ahead_trade' AND metric = '日前申报均价')
		      )
		ORDER BY run_date, dataset, metric
	`, from, to)
	if err != nil {
		return nil, errors.Wrap(err, "查询官方日频指标失败")
	}
	return rows, nil
}
