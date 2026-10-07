package internal

import (
	"common/c_base"
	"context"
	"math"
	"testing"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/tsdb"
)

// 这是**会删数据**的功能，所以用真实 promtsdb 在临时目录里验一遍：
// 判断标准不是「没报错」，而是「该死的死了、不该动的一条没少」。

func countSamples(t *testing.T, db *tsdb.DB, matchers ...*labels.Matcher) int {
	t.Helper()
	q, err := db.Querier(context.Background(), 0, math.MaxInt64)
	if err != nil {
		t.Fatalf("创建 Querier 失败: %v", err)
	}
	defer q.Close()

	ss, _, err := q.Select(false, nil, matchers...)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	n := 0
	for ss.Next() {
		it := ss.At().Iterator()
		for it.Next() {
			n++
		}
		if err := it.Err(); err != nil {
			t.Fatalf("遍历样本失败: %v", err)
		}
	}
	return n
}

func TestDeleteBeforeKeepsPriceAndRecentData(t *testing.T) {
	dir := t.TempDir()
	db, err := tsdb.Open(dir, nil, nil, nil)
	if err != nil {
		t.Fatalf("打开 TSDB 失败: %v", err)
	}
	defer func() { _ = db.Close() }()

	p := &promDB{ctx: context.Background(), db: db}

	old := time.Now().AddDate(0, 0, -40).UnixMilli()
	recent := time.Now().AddDate(0, 0, -1).UnixMilli()

	app := db.Appender()
	add := func(l labels.Labels, ts int64, v float64) {
		if _, err := app.Add(l, ts, v); err != nil {
			t.Fatalf("写入样本失败: %v", err)
		}
	}
	// 40 天前：设备、协议、系统指标(host)、电价
	add(buildDeviceLabels("d1", "power"), old, 1)
	add(buildProtocolLabels("modbus_1", "d1", "reg"), old, 2)
	add(buildSystemLabels("host", "cpu"), old, 3)
	add(buildSystemLabels(PriceMeasurement, "price"), old, 400)
	// 1 天前：设备数据（新数据不该被碰）
	add(buildDeviceLabels("d1", "power"), recent, 5)
	if err := app.Commit(); err != nil {
		t.Fatalf("提交失败: %v", err)
	}

	deviceMatchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, LabelNameMetric, MetricNameEmsMetric),
		labels.MustNewMatcher(labels.MatchEqual, LabelNameType, string(c_base.StorageTypeDevice)),
	}
	priceMatchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, LabelNameMetric, MetricNameEmsMetric),
		labels.MustNewMatcher(labels.MatchEqual, LabelNameType, string(c_base.StorageTypeSystem)),
		labels.MustNewMatcher(labels.MatchEqual, LabelNameID, PriceMeasurement),
	}

	before := countSamples(t, db, deviceMatchers...)
	if before != 2 {
		t.Fatalf("准备阶段设备样本应为 2 条，实际 %d", before)
	}

	// 保留 30 天：40 天前的该走，1 天前的该留
	if err := p.DeleteBefore(time.Now().AddDate(0, 0, -30), c_base.StorageTypeDevice, c_base.StorageTypeProtocol, c_base.StorageTypeSystem); err != nil {
		t.Fatalf("清理失败: %v", err)
	}

	if got := countSamples(t, db, deviceMatchers...); got != 1 {
		t.Errorf("设备样本应只剩 1 条（1 天前那条），实际 %d", got)
	}
	if got := countSamples(t, db, priceMatchers...); got == 0 {
		t.Error("**电价数据被删掉了** —— 长周期预测/复盘/反演会因此失去历史价格")
	}
	// 协议与系统指标的老数据也该走
	protoMatchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, LabelNameMetric, MetricNameEmsMetric),
		labels.MustNewMatcher(labels.MatchEqual, LabelNameType, string(c_base.StorageTypeProtocol)),
	}
	if got := countSamples(t, db, protoMatchers...); got != 0 {
		t.Errorf("协议老数据应被清掉，实际还剩 %d", got)
	}
	hostMatchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, LabelNameMetric, MetricNameEmsMetric),
		labels.MustNewMatcher(labels.MatchEqual, LabelNameType, string(c_base.StorageTypeSystem)),
		labels.MustNewMatcher(labels.MatchEqual, LabelNameID, "host"),
	}
	if got := countSamples(t, db, hostMatchers...); got != 0 {
		t.Errorf("系统指标(host)老数据应被清掉，实际还剩 %d", got)
	}
}

// 清理范围必须是「早于截止时间」，不能把新数据一起带走。
func TestDeleteBeforeDoesNotTouchRecentData(t *testing.T) {
	dir := t.TempDir()
	db, err := tsdb.Open(dir, nil, nil, nil)
	if err != nil {
		t.Fatalf("打开 TSDB 失败: %v", err)
	}
	defer func() { _ = db.Close() }()

	p := &promDB{ctx: context.Background(), db: db}
	app := db.Appender()
	if _, err := app.Add(buildDeviceLabels("d1", "power"), time.Now().Add(-time.Hour).UnixMilli(), 1); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := app.Commit(); err != nil {
		t.Fatalf("提交失败: %v", err)
	}

	// 保留 30 天，1 小时前的数据必须原样在
	if err := p.DeleteBefore(time.Now().AddDate(0, 0, -30), c_base.StorageTypeDevice); err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, LabelNameMetric, MetricNameEmsMetric),
		labels.MustNewMatcher(labels.MatchEqual, LabelNameType, string(c_base.StorageTypeDevice)),
	}
	if got := countSamples(t, db, matchers...); got != 1 {
		t.Errorf("新数据不该被清掉，实际剩 %d 条", got)
	}
}
