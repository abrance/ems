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
// 复核状态优先：已确认（origin=manual）行默认**不参与替换**——保留原值与原状态，
// 本次解析重新提取到同键的值直接丢弃（结果计入 Preserved）。只有 force=true 才
// 「以 OCR 为准」覆盖它们，覆盖前逐条写审计。
//
// 为什么默认不覆盖：解析器升级后跑一次解析不该静默清掉别人当天已核对过的判断
// （实测发生过：一次重跑重置 24 项确认，值未变但复核状态丢失）。
func (s *sSpotExtractServiceImpl) ReplaceMetrics(ctx context.Context, reportDate string, metrics []*s_db_model.SSpotMetricModel, force bool) (*s_db_basic.SpotMetricReplaceResult, error) {
	result := &s_db_basic.SpotMetricReplaceResult{}
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		newValues := make(map[string]*float64, len(metrics))
		for _, m := range metrics {
			v := m.Value
			newValues[metricKey(m.ReportDate, m.RunDate, m.Dataset, m.Scope, m.Metric)] = &v
		}

		var confirmed []*s_db_model.SSpotMetricModel
		if err := tx.Model(s_db_model.TableSpotMetric).Ctx(ctx).
			Where(s_db_model.FieldSpotMetricReportDate, reportDate).
			Where(s_db_model.FieldSpotMetricOrigin, "manual").
			Scan(&confirmed); err != nil {
			return err
		}

		now := gtime.Now()
		keptIDs := make([]int64, 0, len(confirmed))
		if force {
			for _, m := range confirmed {
				newValue := newValues[metricKey(m.ReportDate, m.RunDate, m.Dataset, m.Scope, m.Metric)]
				row := g.Map{
					"report_date": m.ReportDate, "run_date": m.RunDate, "dataset": m.Dataset,
					"scope": m.Scope, "metric": m.Metric, "old_value": m.Value,
					"new_value":  newValue,
					"old_origin": "manual", "operation": s_db_model.SpotMetricAuditOverwrite,
					"operator": "extract", "created_at": now,
				}
				if _, err := tx.Model(s_db_model.TableSpotMetricAudit).Ctx(ctx).Data(row).Insert(); err != nil {
					return err
				}
				result.Overwritten = append(result.Overwritten, s_db_basic.SpotMetricOverwrite{
					Key: s_db_basic.SpotMetricKey{
						ReportDate: m.ReportDate, RunDate: m.RunDate, Dataset: m.Dataset,
						Scope: m.Scope, Metric: m.Metric, SourceLabel: m.Source,
					},
					OldValue: m.Value,
					NewValue: newValue,
				})
			}
		} else {
			// 留住的已确认行不能被新插入的同键行撞唯一键，所以先把这次解析到的同键值丢掉
			keys := make(map[string]bool, len(confirmed))
			for _, m := range confirmed {
				key := metricKey(m.ReportDate, m.RunDate, m.Dataset, m.Scope, m.Metric)
				keys[key] = true
				keptIDs = append(keptIDs, m.Id)
				// 保留归保留，但重算值不一致要说出来：否则解析器修好的提取缺陷
				// 永远不会落到已确认行上，而没人知道那些值已经旧了
				if newValue := newValues[key]; newValue != nil && *newValue != m.Value {
					result.PreservedDiff = append(result.PreservedDiff, s_db_basic.SpotMetricOverwrite{
						Key: s_db_basic.SpotMetricKey{
							ReportDate: m.ReportDate, RunDate: m.RunDate, Dataset: m.Dataset,
							Scope: m.Scope, Metric: m.Metric, SourceLabel: m.Source,
						},
						OldValue: m.Value,
						NewValue: newValue,
					})
				}
			}
			metrics = dropConfirmedIncoming(keys, metrics)
			result.Preserved = len(confirmed)
		}

		del := tx.Model(s_db_model.TableSpotMetric).Ctx(ctx).
			Where(s_db_model.FieldSpotMetricReportDate, reportDate)
		if len(keptIDs) > 0 {
			del = del.WhereNotIn("id", keptIDs)
		}
		if _, err := del.Delete(); err != nil {
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
	return result, nil
}

// dropConfirmedIncoming 丢掉与已确认行同键的新提取值（纯函数，便于单测）
func dropConfirmedIncoming(confirmedKeys map[string]bool, incoming []*s_db_model.SSpotMetricModel) []*s_db_model.SSpotMetricModel {
	kept := make([]*s_db_model.SSpotMetricModel, 0, len(incoming))
	for _, m := range incoming {
		if confirmedKeys[metricKey(m.ReportDate, m.RunDate, m.Dataset, m.Scope, m.Metric)] {
			continue
		}
		kept = append(kept, m)
	}
	return kept
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

// ConfirmMetrics 批量确认：把该日报所有未确认项（origin != manual，即 ocr 提取值与 cv 预填值）
// 一并标记为已确认（origin=manual）并逐条写审计。dataset 为空表示全部数据集。
//
// 为什么不再只认 cv：复核状态的语义是「整天解析结果经人确认」（origin=manual 即已确认），
// 若只翻转 cv，则 ocr 行永远确认不了，整篇确认后的「已确认」状态无处存放。
func (s *sSpotExtractServiceImpl) ConfirmMetrics(ctx context.Context, reportDate, dataset, operator string) (int, error) {
	count := 0
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		query := tx.Model(s_db_model.TableSpotMetric).Ctx(ctx).
			Where(s_db_model.FieldSpotMetricReportDate, reportDate)
		if dataset != "" {
			query = query.Where(s_db_model.FieldSpotMetricDataset, dataset)
		}
		// 未确认判定放在内存里做：origin 可能是空串（老库遗留行），空串同样算未确认
		var all []*s_db_model.SSpotMetricModel
		if err := query.Scan(&all); err != nil {
			return err
		}
		rows := make([]*s_db_model.SSpotMetricModel, 0, len(all))
		for _, r := range all {
			if r.Origin == "manual" {
				continue
			}
			rows = append(rows, r)
		}
		if len(rows) == 0 {
			return nil
		}
		now := gtime.Now()
		ids := make([]int64, 0, len(rows))
		audits := make([]g.Map, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.Id)
			audits = append(audits, g.Map{
				"report_date": r.ReportDate, "run_date": r.RunDate, "dataset": r.Dataset,
				"scope": r.Scope, "metric": r.Metric,
				"old_value": r.Value, "new_value": r.Value, "old_origin": r.Origin,
				"operation": s_db_model.SpotMetricAuditConfirm, "operator": operator, "created_at": now,
			})
		}
		if _, err := tx.Model(s_db_model.TableSpotMetric).Ctx(ctx).
			WhereIn("id", ids).
			Data(g.Map{
				s_db_model.FieldSpotMetricOrigin:   "manual",
				s_db_model.FieldSpotMetricEditedBy: operator,
				s_db_model.FieldSpotMetricEditedAt: now,
				"updated_at":                       now,
			}).Update(); err != nil {
			return err
		}
		if _, err := tx.Model(s_db_model.TableSpotMetricAudit).Ctx(ctx).Data(audits).Insert(); err != nil {
			return err
		}
		count = len(rows)
		return nil
	})
	if err != nil {
		return 0, errors.Wrap(err, "确认指标失败")
	}
	return count, nil
}

// CountMetricsByReport 按日报日统计未复核/已确认条数：一次 GROUP BY，列表页不必逐篇查
func (s *sSpotExtractServiceImpl) CountMetricsByReport(ctx context.Context) (map[string]s_db_basic.SpotMetricCounts, error) {
	var rows []struct {
		ReportDate string `orm:"report_date"`
		Origin     string `orm:"origin"`
		N          int    `orm:"n"`
	}
	err := g.Model(s_db_model.TableSpotMetric).Ctx(ctx).
		Fields(s_db_model.FieldSpotMetricReportDate + ", " + s_db_model.FieldSpotMetricOrigin + ", count(*) as n").
		Group(s_db_model.FieldSpotMetricReportDate + ", " + s_db_model.FieldSpotMetricOrigin).
		Scan(&rows)
	if err != nil {
		return nil, errors.Wrap(err, "统计指标复核状态失败")
	}
	out := make(map[string]s_db_basic.SpotMetricCounts, len(rows))
	for _, r := range rows {
		c := out[r.ReportDate]
		if r.Origin == "manual" {
			c.Confirmed += r.N
		} else {
			c.Pending += r.N
		}
		out[r.ReportDate] = c
	}
	return out, nil
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
