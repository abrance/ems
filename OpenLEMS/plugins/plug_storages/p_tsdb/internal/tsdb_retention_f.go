package internal

import (
	"common/c_base"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
)

// 数据保留清理。
//
// 三条设置（device / system / log 保留天数）原先只写不读，谁都不清理，
// 于是「存储管理」里改那三个数字不会删任何数据。这里让它真的生效。

// PriceMeasurement 电价数据在系统类里的 measurement 名。
//
// 电价是**市场数据**，不是系统运行指标：它用 buildSystemLabels("price", ...) 存在
// type=system 下。按系统保留天数一起删掉会毁掉长周期电价预测、预测复盘、策略反演
// 依赖的历史价格（那些功能要的是好几年的价），所以清理系统类时必须显式排除它。
const PriceMeasurement = "price"

// retentionMatchers 构造按存储类型清理用的标签选择器。
func retentionMatchers(storageType c_base.StorageType) []*labels.Matcher {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, LabelNameMetric, MetricNameEmsMetric),
		labels.MustNewMatcher(labels.MatchEqual, LabelNameType, string(storageType)),
	}
	if storageType == c_base.StorageTypeSystem {
		matchers = append(
			matchers,
			labels.MustNewMatcher(labels.MatchNotEqual, LabelNameID, PriceMeasurement),
		)
	}
	return matchers
}

// DeleteBefore 删除 before 之前的历史数据，按存储类型筛选。
//
// 只打 tombstone 不会释放磁盘 —— CleanTombstones 会重写块，才真正回收空间。
// 这个功能的目的就是别把磁盘填满，所以清理完顺手整理一次。
func (p *promDB) DeleteBefore(before time.Time, types ...c_base.StorageType) error {
	if p.db == nil {
		return errors.New("存储未初始化")
	}
	maxt := before.UnixMilli()
	for _, t := range types {
		if err := p.db.Delete(0, maxt, retentionMatchers(t)...); err != nil {
			return errors.Wrapf(err, "清理 %s 历史数据失败", t)
		}
	}
	if err := p.db.CleanTombstones(); err != nil {
		return errors.Wrap(err, "整理 tombstones 失败（数据已标记删除，只是磁盘暂未回收）")
	}
	return nil
}
