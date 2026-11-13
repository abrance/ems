package reportdaily

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// 口径的机器证据：D 这条链路**绝不写** gd_spot_metric。
//
// 用户硬规则（2026-11-13）：OCR 落独立表 gd_report_daily，只存分电源均价 + 申报结构，
// 绝不写 gd_spot_metric；旧链路 POST /api/spot-report/extract 不动。
//
// 「绝不写」这件事没有任何东西会在运行时拦住：只要有人在包里多写一句
// s_db.GetSpotExtractService().ReplaceMetrics(...)，编译通过、测试全绿、口径已经破了。
// 所以由 CI 挡：本包所有 *.go 源码里不许出现那张表的表名或它的 model/service 名字。
//
// 注释里也不许出现：允许注释写会让「先写注释再补代码」这条路径变得顺畅，
// 而这张表的正确称呼是「旧链路」「官方直采」，不需要点名。
var forbiddenInReportDaily = []string{
	"TableSpotMetric",
	"SSpotMetricModel",
	"gd_spot_metric",
	"GetSpotExtractService",
	"ReplaceMetrics",
	"ListMetricsByRunDateRange",
}

// TestReportDailyNeverWritesSpotMetric 本包源码里出现禁用符号 → 红。
func TestReportDailyNeverWritesSpotMetric(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("取当前目录失败: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取包目录失败: %v", err)
	}

	var offenders []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if strings.HasSuffix(e.Name(), "_test.go") {
			// 测试文件本身要写禁用字符串才能断言，跳过；它不参与生产代码
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", path, err)
		}
		body := string(raw)
		for _, bad := range forbiddenInReportDaily {
			if strings.Contains(body, bad) {
				offenders = append(offenders, e.Name()+" 出现 "+bad)
			}
		}
	}
	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Fatalf("reportdaily 这条链路不许碰 gd_spot_metric（口径：OCR 落独立表 gd_report_daily）：\n  %s\n"+
			"官方值只读，走 s_db.GetReportDailyService().ListOfficialDaily；"+
			"要写日报原值走 UpsertBatch", strings.Join(offenders, "\n  "))
	}
}
