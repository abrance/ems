package gdmetric

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// 门禁：谁还允许 import 这个已停更的 CSV 通道。
//
// 用户硬规则（2026-11-13）：官方覆盖的能力一律读官方，CSV 路径直接删；官方没覆盖的
// 能力要明说用不了，不许拿 CSV 假装还在工作。所以「又有人把 gdmetric 接回某个日频
// 数字上」这件事必须由 CI 挡，而不是靠评审记得。
//
// 规则：只有下面白名单里的文件可以 import gdmetric，且每个都必须是
//   - 96 点能力本身（分时曲线/供给曲线/相似日/分时预测），或
//   - 冻结通道的显式 opt-in 入口（名字里带 Frozen / allowFrozen），或
//   - 这个通道的测试。
//
// 新增文件要接 96 点之前，先问：能不能用官方日频？不能，才加进白名单并在 PR 里说明原因。
var allowedGdmetricImporters = map[string]string{
	// API 类型（时间序列/曲线结构体定义，不含取数）
	"api/spotreport/v1/gd_metric.go":              "快照响应类型",
	"api/spotreport/v1/gd_metric_series.go":       "96 点序列响应类型",
	"api/spotreport/v1/gd_metric_similar_days.go": "相似日响应类型（能力已停用）",
	"api/spotreport/v1/gd_metric_supply.go":       "供给曲线响应类型（能力已停用）",
	// 控制器：96 点入口（默认返回停更错误，allowFrozen 才读冻结通道）
	"internal/controller/spotreport/gd_metric_series.go":       "96 点序列 + allowFrozen opt-in",
	"internal/controller/spotreport/gd_metric_similar_days.go": "相似日（已停用，返回停更错误）",
	"internal/controller/spotreport/gd_metric_supply.go":       "供给曲线（已停用，返回停更错误）",
	"internal/controller/spotreport/spotreport_v1.go":          "快照 + allowFrozen opt-in",
	// 预测/回测：96 点能力
	"internal/logic/forecast/price_intraday.go": "96 点分时预测（官方无此粒度，已停用）",
	"internal/logic/forecast/backtest.go":       "96 点回测真值（allowStale 的历史演示腿）",
	// 测试
	"internal/logic/forecast/backtest_freshness_test.go": "96 点真值新鲜度单测",
	"internal/logic/forecast/backtest_pair_test.go":      "96 点配对回归",
	"internal/logic/forecast/price_realdata_test.go":     "96 点真数据体检",
}

// moduleRoot 从测试文件往上找到 go.mod 所在目录（application/）。
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("取当前目录失败: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("找不到 go.mod")
		}
		dir = parent
	}
}

// TestNoNewGdmetricImporters 任何白名单外的文件 import gdmetric → 红。
func TestNoNewGdmetricImporters(t *testing.T) {
	root := moduleRoot(t)
	const imp = "application/internal/logic/gdmetric"
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == "vendor" || name == "gdmetric" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		body := string(raw)
		if !strings.Contains(body, `"`+imp+`"`) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if _, ok := allowedGdmetricImporters[rel]; !ok {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历源码失败: %v", err)
	}
	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Fatalf("这些文件 import 了已停更的 gdmetric（第三方 CSV 通道）：\n  %s\n"+
			"官方日频请用 spotmarket.LoadBoard / GET /api/spot-market/board；"+
			"确实只能走 96 点的，把文件加进 allowedGdmetricImporters 并在 PR 里说明为什么官方覆盖不了",
			strings.Join(offenders, "\n  "))
	}
}

// TestRetiredEntryPointsFailClosed 守卫本身要有效：默认入口必须报停更，而不是返回数据。
//
// 这条同时挡住「有人为了让自己页面好看，把 guard 删了」：删掉守卫这里立刻红。
func TestRetiredEntryPointsFailClosed(t *testing.T) {
	ctx := t.Context()
	if _, err := GetSeries(ctx, "2026-09-01", "2026-09-02", KindDayAhead); !IsChannelRetired(err) {
		t.Fatalf("GetSeries 应返回停更错误，实际 err=%v", err)
	}
	if _, err := GetRealSeries(ctx, "2026-09-01", "2026-09-02", KindDayAhead); !IsChannelRetired(err) {
		t.Fatalf("GetRealSeries 应返回停更错误，实际 err=%v", err)
	}
	if _, err := GetSnapshot(ctx, "", "", ""); !IsChannelRetired(err) {
		t.Fatalf("GetSnapshot 应返回停更错误，实际 err=%v", err)
	}
	if _, err := GetSupplyCurve(ctx, "2026-09-01", "2026-09-02", KindDayAhead, SupplyOptions{}); !IsChannelRetired(err) {
		t.Fatalf("GetSupplyCurve 应返回停更错误，实际 err=%v", err)
	}
	if _, err := SimilarDays(ctx, "2026-09-02", KindDayAhead, SimilarOptions{}); !IsChannelRetired(err) {
		t.Fatalf("SimilarDays 应返回停更错误，实际 err=%v", err)
	}
	// 错误里必须带具体日期，页面才能说「数据止于哪天」
	var retiredErr *ChannelRetiredError
	if err := retired("测试"); !asRetired(err, &retiredErr) || retiredErr.DataAsOf == "" {
		t.Fatalf("停更错误必须带 DataAsOf，实际 %+v", retiredErr)
	}
}

func asRetired(err error, target **ChannelRetiredError) bool {
	e, ok := err.(*ChannelRetiredError)
	if ok {
		*target = e
	}
	return ok
}
