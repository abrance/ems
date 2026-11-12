package impl

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"common/c_enum"
	"s_db/s_db_model"
)

// 插入路径开销实测：1000 行库里跑 N 次写入，分别量
//
//	baseline = model.Create（等价 trim 之前的写入路径）
//	withTrim = svc.CreateAlarmHistory（Insert + 一条 DELETE + 清计数缓存）
//
// 默认跳过（每次约 0.3s，没必要进常规回归）；跑法见 EMS_BENCH 环境变量：
//
//	EMS_BENCH=1 go test ./internal/impl/ -run TestAlarmHistoryInsertCost -v
func TestAlarmHistoryInsertCost(t *testing.T) {
	if os.Getenv("EMS_BENCH") == "" {
		t.Skip("设置 EMS_BENCH=1 才跑插入路径开销实测")
	}

	const iterations = 100

	measure := func(name string, ctx context.Context, insert func(int) error) {
		t.Helper()
		durations := make([]time.Duration, 0, iterations)
		for i := 0; i < iterations; i++ {
			start := time.Now()
			if err := insert(i); err != nil {
				t.Fatalf("%s 第 %d 次写入失败: %v", name, i+1, err)
			}
			durations = append(durations, time.Since(start))
		}
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		sum := time.Duration(0)
		for _, d := range durations {
			sum += d
		}
		t.Logf("%s: n=%d 均值=%.3fms p50=%.3fms p99=%.3fms 最大=%.3fms",
			name, iterations,
			float64(sum)/float64(iterations)/float64(time.Millisecond),
			float64(durations[iterations/2])/float64(time.Millisecond),
			float64(durations[iterations*99/100])/float64(time.Millisecond),
			float64(durations[iterations-1])/float64(time.Millisecond),
		)
	}

	// 基线：只 Insert，不 trim。
	ctxBase := newAlarmHistoryTestDB(t)
	seedAlarmHistory(t, ctxBase, alarmHistoryKeepRows)
	at := time.Now()
	measure("baseline(model.Create)", ctxBase, func(i int) error {
		return (&s_db_model.SAlarmHistoryModel{
			DeviceId: "dev-1", SourceDeviceId: "dev-1", Point: "Voltage",
			PointName: "电压过高", Level: "Warn",
			Detail: fmt.Sprintf("bench-%d", i), TriggerAt: &at,
		}).Create(ctxBase)
	})

	// 只 Insert + trim（不含清计数缓存），用来把 DELETE 本身的代价单独量出来。
	ctxInsertTrim := newAlarmHistoryTestDB(t)
	seedAlarmHistory(t, ctxInsertTrim, alarmHistoryKeepRows)
	trimModel := &s_db_model.SAlarmHistoryModel{}
	measure("insert+trim(no cache)", ctxInsertTrim, func(i int) error {
		if err := (&s_db_model.SAlarmHistoryModel{
			DeviceId: "dev-1", SourceDeviceId: "dev-1", Point: "Voltage",
			PointName: "电压过高", Level: "Warn",
			Detail: fmt.Sprintf("bench-%d", i), TriggerAt: &at,
		}).Create(ctxInsertTrim); err != nil {
			return err
		}
		_, err := trimModel.TrimToNewest(ctxInsertTrim, alarmHistoryKeepRows)
		return err
	})

	// 带 trim：走真实写入路径。
	ctxTrim := newAlarmHistoryTestDB(t)
	seedAlarmHistory(t, ctxTrim, alarmHistoryKeepRows)
	svc := GetAlarmService()
	point := testAlarmPoint()
	measure("withTrim(CreateAlarmHistory)", ctxTrim, func(i int) error {
		return svc.CreateAlarmHistory(ctxTrim, "dev-1", "dev-1", point, c_enum.EAlarmLevelWarn,
			fmt.Sprintf("bench-%d", i), at)
	})

	// 表行数必须仍被压在 1000：证明这 100 次写入期间 trim 一直在生效。
	count, err := (&s_db_model.SAlarmHistoryModel{}).GetCount(ctxTrim)
	if err != nil {
		t.Fatalf("GetCount 失败: %v", err)
	}
	if count != alarmHistoryKeepRows {
		t.Fatalf("跑完 %d 次写入后表应有 %d 行，实际 %d", iterations, alarmHistoryKeepRows, count)
	}
}
