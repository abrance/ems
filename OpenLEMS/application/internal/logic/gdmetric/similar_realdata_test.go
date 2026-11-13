package gdmetric

import (
	"context"
	"testing"
)

// 真数据体检：合成曲线只能证明「公式按定义算了」，证明不了「挑出来的日子在业务上说得过去」。
// 没有本地 CSV 时跳过（CI 上就是跳过）。
//
// 基线（目标日 2026-08-31，候选约 1790 天）：顶尖相似日应明显高于 50%
// （形态相关下，随机一天期望就是 50%），且相似度必须单调不增。公式一改就把
// 「谁都不像」退化成「都差不多」的话，这个测试会先炸。
func TestSimilarDaysRealData(t *testing.T) {
	ctx := context.Background()
	initGlobal(ctx)
	if !global.localCSVReady() {
		t.Skip("本地没有 gd_metric.csv 缓存，跳过")
	}

	res, err := FrozenSimilarDays(ctx, "2026-08-31", KindDayAhead, SimilarOptions{TopN: 5})
	if err != nil {
		t.Fatalf("找相似日失败: %v", err)
	}
	if res.Candidates < 100 {
		t.Fatalf("参与比较的历史日只有 %d 天，CSV 或完整性筛选有问题", res.Candidates)
	}
	if len(res.Days) != 5 {
		t.Fatalf("应返回 5 条，得到 %d", len(res.Days))
	}
	for i, d := range res.Days {
		if d.Similarity < 0 || d.Similarity > 100 {
			t.Errorf("%s 相似度越界: %v", d.Date, d.Similarity)
		}
		if i > 0 && d.Similarity > res.Days[i-1].Similarity+1e-9 {
			t.Errorf("相似度没有降序: %v", res.Days)
		}
		if d.Date == res.TargetDate {
			t.Errorf("目标日混进来了: %s", d.Date)
		}
	}
	if res.Days[0].Similarity <= 60 {
		t.Errorf("最相似的日子只有 %.1f%%，形态相关可能退化了（随机水平约 50%%）", res.Days[0].Similarity)
	}
	t.Logf("目标日 %s：候选 %d 天，%.0fms，TOP5 = %v", res.TargetDate, res.Candidates, res.TimeMs, similarBrief(res.Days))

	// 换市场（实时价）与「只看负荷形态」也要能跑
	if _, err := FrozenSimilarDays(ctx, "2026-08-31", KindRealtime, SimilarOptions{TopN: 3, WLoad: 1}); err != nil {
		t.Errorf("实时价 + 只看负荷失败: %v", err)
	}
	// 不给日期 = 最新**有实际值且 96 点齐全**的运行日（最新运行日往往只有电查查预测占位）
	snap, snapErr := GetFrozenSnapshot(ctx, "", "", "")
	if snapErr != nil || snap == nil {
		t.Fatalf("取快照失败: %v", snapErr)
	}
	latest, err := FrozenSimilarDays(ctx, "", KindDayAhead, SimilarOptions{TopN: 1})
	if err != nil {
		t.Errorf("不给日期应回落到最新有实际值的运行日: %v", err)
	} else if latest.TargetDate == "" {
		t.Error("回落时 targetDate 不应为空")
	} else if latest.TargetDate > snap.LatestRealDate {
		t.Errorf("回落到了 %s，晚于最新实际值日 %s", latest.TargetDate, snap.LatestRealDate)
	}
}

func similarBrief(days []SimilarDay) []string {
	out := make([]string, 0, len(days))
	for _, d := range days {
		out = append(out, d.Date)
	}
	return out
}
