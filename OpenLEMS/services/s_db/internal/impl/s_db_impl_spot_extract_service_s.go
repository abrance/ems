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

// spotOriginManual 人工确认/修订过的 origin 值。
//
// 取值与三组口径都在 s_db_model 里定（SpotOriginsTrusted / Unreviewed / Inventory），
// 本文件只做别名 —— 原先这里自己存一份字符串常量，导致「业务白名单」与「数据面统计」
// 两套口径散在包与包之间，2026-11-14 因此漏读 453 行 origin=cv。
const (
	spotOriginManual = s_db_model.SpotOriginManual
	spotOriginPmAPI  = s_db_model.SpotOriginPmAPI
	// spotOriginVoid 已被官方直采值取代的旧值：保留可查，但**永远**不再参与确认/统计
	spotOriginVoid = s_db_model.SpotOriginVoid
)

// spotOriginConfirmed 业务口径白名单：能进聚合的那两个 origin。
// **不得**用它做行数/覆盖率统计（那会把 ocr/cv 漏掉，见 s_db_model 里的 const 注释）。
var spotOriginConfirmed = s_db_model.SpotOriginsTrusted

// isConfirmedOrigin 该 origin 是否已是可信值（无需再复核）。与 application 侧
// spotreport.IsConfirmed 同一口径；两边都能"确认"数据，口径必须一致。
func isConfirmedOrigin(origin string) bool {
	return origin == spotOriginManual || origin == spotOriginPmAPI
}

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
// 复核状态优先：已确认（origin 为 manual / pm-api）行**永远不参与替换**——保留原值与原状态，
// 本次解析重新提取到同键的值直接丢弃（结果计入 Preserved）。force=true 也一样：它只对
// 未复核的 ocr/cv 值有意义，权威行改为逐条记一条告警（结果计入 Skipped）。
//
// 为什么默认不覆盖：解析器升级后跑一次解析不该静默清掉别人当天已核对过的判断
// （实测发生过：一次重跑重置 24 项确认，值未变但复核状态丢失）。
//
// 为什么 force 也不能覆盖：2026-11-12 用 force=true 补采 10-16 / 10-25 时，各 8 行
// origin='pm-api' 被整段删掉（旧实现 force 分支不留 keptIDs，先删后插只写回本次 OCR
// 提取到的键），只剩 16 条 overwritten_by_extract 审计。官方直采是唯一权威源，
// 任何重跑都不该比它更有权威。
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
			WhereIn(s_db_model.FieldSpotMetricOrigin, spotOriginConfirmed).
			Scan(&confirmed); err != nil {
			return err
		}

		now := gtime.Now()
		keptIDs := make([]int64, 0, len(confirmed))
		// 留住的已确认行不能被新插入的同键行撞唯一键，所以先把这次解析到的同键值丢掉
		keys := make(map[string]bool, len(confirmed))
		for _, m := range confirmed {
			key := metricKey(m.ReportDate, m.RunDate, m.Dataset, m.Scope, m.Metric)
			keys[key] = true
			keptIDs = append(keptIDs, m.Id)
			if force {
				// force 只对未复核的 ocr/cv 值有意义：官方直采（pm-api）与人工确认（manual）
				// 是权威值，即使 force=true 也不覆盖、不删，只留一条告警。
				result.Skipped = append(result.Skipped, s_db_basic.SpotMetricOverwrite{
					Key: s_db_basic.SpotMetricKey{
						ReportDate: m.ReportDate, RunDate: m.RunDate, Dataset: m.Dataset,
						Scope: m.Scope, Metric: m.Metric, SourceLabel: m.Source,
					},
					OldValue: m.Value,
					NewValue: newValues[key],
					Origin:   m.Origin,
				})
				continue
			}
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
				m.Origin = s_db_model.SpotOriginOCR
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

// ConfirmMetrics 批量确认：把该日报所有未确认项（既非 manual 也非 pm-api，即 ocr 提取值与 cv 预填值）
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
			// 已确认（manual）与官方直采（pm-api）都不该被「确认」改写成 manual：
			// 后者一旦被翻成 manual 就丢了来源标记，再也分不出哪条是官方直采
			if isConfirmedOrigin(r.Origin) {
				continue
			}
			// 作废行更不能被「确认」复活：它已经被官方值取代，复活等于把旧值又当权威值
			if r.Origin == spotOriginVoid {
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
		// 作废行既不算已确认也不算待复核：它不在任何复核队列里
		if r.Origin == spotOriginVoid {
			continue
		}
		c := out[r.ReportDate]
		if isConfirmedOrigin(r.Origin) {
			c.Confirmed += r.N
		} else {
			c.Pending += r.N
		}
		out[r.ReportDate] = c
	}
	return out, nil
}

// CountMetricsByOrigin 按 origin 分组数全表行数，**不加任何白名单**。
//
// 这个方法是「数据面」专用的：库里有几行就读出几行。业务口径的白名单定义在
// s_db_model.SpotOriginsTrusted，拿它做行数统计会漏掉 ocr/cv（2026-11-14 因此
// 误报 453 行 cv 档位价丢失）。老库里 origin 为 NULL 的行归入空串 key，不丢。
func (s *sSpotExtractServiceImpl) CountMetricsByOrigin(ctx context.Context) (map[string]int, error) {
	var rows []struct {
		Origin string `orm:"origin"`
		N      int    `orm:"n"`
	}
	// 用 COALESCE 把 NULL 归一成空串：gf 的 Scan 对 NULL string 会报错或给零值，
	// 而这两种都看不出「有行没被归组」—— 空串 key 至少能让它显形。
	err := g.Model(s_db_model.TableSpotMetric).Ctx(ctx).
		Fields("COALESCE(" + s_db_model.FieldSpotMetricOrigin + ", '') AS origin, count(*) as n").
		Group("COALESCE(" + s_db_model.FieldSpotMetricOrigin + ", '')").
		Scan(&rows)
	if err != nil {
		return nil, errors.Wrap(err, "按 origin 统计指标行数失败")
	}
	out := make(map[string]int, len(rows))
	for _, r := range rows {
		out[r.Origin] += r.N
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
		// 多取一列 created_at 的原始文本：区分「带时区」与导入脚本写的「无时区本地时间」
		// （COALESCE 只是为了老库里可能存在的 NULL 行不报扫描错误）
		Fields(s_db_model.TableSpotMetricAudit+".*", "COALESCE(CAST(created_at AS TEXT),'') AS created_at_text").
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
		// 作废行（被 pm-api 取代的 OCR 旧值）保留在库里可审计，但不再进任何页面/统计。
		// 老库可能有 origin 为 NULL 的行，NULL <> 'void' 是 NULL，会误删这些行，所以显式带 IS NULL。
		Where(s_db_model.FieldSpotMetricOrigin+" IS NULL OR "+s_db_model.FieldSpotMetricOrigin+" <> ?", spotOriginVoid).
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

func (s *sSpotExtractServiceImpl) ListMetricsByRunDateRange(ctx context.Context, from, to string) ([]*s_db_model.SSpotMetricModel, error) {
	var list []*s_db_model.SSpotMetricModel
	err := g.Model(s_db_model.TableSpotMetric).Ctx(ctx).
		WhereGTE(s_db_model.FieldSpotMetricRunDate, from).
		WhereLTE(s_db_model.FieldSpotMetricRunDate, to).
		WhereIn(s_db_model.FieldSpotMetricOrigin, spotOriginConfirmed).
		OrderAsc(s_db_model.FieldSpotMetricRunDate).
		OrderAsc(s_db_model.FieldSpotMetricDataset).
		OrderAsc(s_db_model.FieldSpotMetricScope).
		Scan(&list)
	if err != nil {
		return nil, errors.Wrap(err, "按运行日区间查询现货指标失败")
	}
	return list, nil
}

func (s *sSpotExtractServiceImpl) LatestMetricDate(ctx context.Context, dataset string) (string, error) {
	// 单字段扫进 []string 会被 gf 序列化成整行 JSON，按行结构体接。
	var rows []struct {
		RunDate string `orm:"run_date"`
	}
	m := g.Model(s_db_model.TableSpotMetric).Ctx(ctx).
		WhereIn(s_db_model.FieldSpotMetricOrigin, spotOriginConfirmed)
	if dataset != "" {
		m = m.Where(s_db_model.FieldSpotMetricDataset, dataset)
	}
	err := m.Fields(s_db_model.FieldSpotMetricRunDate).
		OrderDesc(s_db_model.FieldSpotMetricRunDate).
		Limit(1).
		Scan(&rows)
	if err != nil {
		return "", errors.Wrap(err, "查询现货指标最新运行日失败")
	}
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].RunDate, nil
}

func (s *sSpotExtractServiceImpl) CountUnconfirmedByRunDateRange(ctx context.Context, from, to string) (int, error) {
	count, err := g.Model(s_db_model.TableSpotMetric).Ctx(ctx).
		WhereGTE(s_db_model.FieldSpotMetricRunDate, from).
		WhereLTE(s_db_model.FieldSpotMetricRunDate, to).
		// 作废行既不算已确认也不算待复核：它在库里可查，但不在任何复核队列里
		Where(s_db_model.FieldSpotMetricOrigin+" IS NULL OR "+s_db_model.FieldSpotMetricOrigin+" <> ?", spotOriginVoid).
		// 老库可能有 origin 为 NULL 的行：SQL 里 NULL <> 'manual' 是 NULL（不算命中），
		// 会漏统计，所以显式带上 IS NULL（与 ConfirmMetrics 的内存判定口径一致）
		Where(s_db_model.FieldSpotMetricOrigin+" IS NULL OR "+s_db_model.FieldSpotMetricOrigin+" NOT IN (?,?)",
			spotOriginManual, spotOriginPmAPI).
		Count()
	if err != nil {
		return 0, errors.Wrap(err, "统计未复核现货指标失败")
	}
	return count, nil
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
