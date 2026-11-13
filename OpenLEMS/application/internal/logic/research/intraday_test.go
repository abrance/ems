package research

import (
	"context"
	"testing"
	"time"
)

// 纯函数部分：滞后天数与日期区间。这两件事错了会直接体现在页面文案上
// （「截至 X · 滞后 N 天」和「哪些天是空态」），所以单独钉住。
func TestLagDays(t *testing.T) {
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, shanghai())
	cases := []struct {
		date string
		want int
	}{
		{"2026-11-13", 0},
		{"2026-11-12", 1},
		{"2026-10-02", 42},
		{"2026-11-14", -1}, // 数据比今天新（不该发生）：负数说明「未来日」，页面要能识别
		{"", -1},
		{"not-a-date", -1},
	}
	for _, c := range cases {
		if got := LagDays(c.date, now); got != c.want {
			t.Errorf("LagDays(%q) = %d，期望 %d", c.date, got, c.want)
		}
	}
}

// 边界必须钉死：恰好 7 天仍是「滞后」（黄色），第 8 天才算过期。
// 阈值写在逻辑里，页面不重算 —— 所以这里红了就说明页面文案会跟着错。
func TestStaleBoundary(t *testing.T) {
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, shanghai())
	for _, c := range []struct {
		date  string
		stale bool
	}{
		{"2026-11-06", false}, // 7 天：滞后
		{"2026-11-05", true},  // 8 天：过期
	} {
		lag := LagDays(c.date, now)
		if got := lag > StaleAfterDays; got != c.stale {
			t.Errorf("%s lag=%d stale=%v，期望 %v", c.date, lag, got, c.stale)
		}
	}
}

func TestDateSpan(t *testing.T) {
	got := DateSpan("2026-11-08", "2026-11-10")
	if len(got) != 3 || got[0] != "2026-11-08" || got[2] != "2026-11-10" {
		t.Fatalf("区间展开错: %v", got)
	}
	if len(DateSpan("2026-11-10", "2026-11-08")) != 0 {
		t.Error("起止颠倒应返回空")
	}
	if len(DateSpan("bad", "2026-11-08")) != 0 {
		t.Error("非法日期应返回空")
	}
}

// 没有本地冻结缓存时（CI / 新机器）：必须返回空 Days + 明确的 missing，
// 而不是报错。页面靠这个渲染「未入库（空态）」，不画 0 线。
func TestLoadWithoutLocalCache(t *testing.T) {
	// 指向一个不存在的缓存路径：initGlobal 只跑一次，这里直接断言行为
	// —— 若本机恰好有缓存，本用例会走有数据分支，用 t.Skip 保持环境无关。
	ctx := context.Background()
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, shanghai())
	res, err := Load(ctx, "2026-11-08", "2026-11-13", now)
	if err != nil {
		t.Fatalf("没有缓存时不该报错（页面要能打开并显示空态）: %v", err)
	}
	if res.Source.Kind != SourceKind {
		t.Errorf("source.kind 必须是 thirdparty，实际 %q", res.Source.Kind)
	}
	if res.AsOf == "" {
		// 无数据：每一天都该进 missing，且 Days 为空
		if len(res.Days) != 0 {
			t.Errorf("没有数据时不该有 Days，实际 %d 天", len(res.Days))
		}
		if len(res.Missing) != 6 {
			t.Errorf("区间内 6 天都该进 missing，实际 %v", res.Missing)
		}
		if res.LagDays != -1 {
			t.Errorf("没有数据时 LagDays 应为 -1，实际 %d", res.LagDays)
		}
		return
	}
	// 本机有缓存：区间右端必须锚到 asOf，不能是今天
	if res.To != res.AsOf && res.To > res.AsOf {
		t.Errorf("默认区间右端应锚到 asOf=%s，实际 %s", res.AsOf, res.To)
	}
	for _, d := range res.Days {
		if len(d.Points) == 0 {
			t.Errorf("%s 进了 Days 却没有点：空态必须走 Missing", d.Date)
		}
	}
}
