package impl

import (
	"context"
	"path/filepath"
	"testing"

	"s_db/s_db_model"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// gd_spot_metric 的行数统计不许套业务白名单。
//
// 2026-11-14 的真实误判：用「origin ∈ {manual, pm-api}」这个**业务**白名单去数地市档位行，
// origin=cv 的 453 行全被漏掉，于是得出「档位价数据丢了」的结论 —— 而线上 453 行一直在位。
// 这类错不是写错一处 SQL，是把两种口径当成一种用，所以补丁治不了；要钉住的是
// 「同一份数据，业务白名单写法与全量写法对不上时必须能当场发现」。
//
// 本用例的判据：数据面统计（CountMetricsByOrigin）的合计必须等于表里的真实行数，
// 且必须把 Trusted / Unreviewed / Void 三组 origin 全部显式认出来 —— 少认一个就是漏读。

// spotOriginInventoryTestDDL 与 s_db_init_f.go 的 gd_spot_metric 一致（列 + 唯一键），
// 否则用例跑在一张比线上宽松的表上，形状错了也发现不了。
const spotOriginInventoryTestDDL = `
	CREATE TABLE gd_spot_metric (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		report_date VARCHAR(10) NOT NULL, run_date VARCHAR(10) NOT NULL,
		dataset VARCHAR(32) NOT NULL, scope VARCHAR(32) NOT NULL DEFAULT '',
		metric VARCHAR(32) NOT NULL, value REAL NOT NULL DEFAULT 0,
		unit VARCHAR(16) NOT NULL DEFAULT '', at_time VARCHAR(8) NOT NULL DEFAULT '',
		text_value VARCHAR(32) NOT NULL DEFAULT '', mom_pct REAL, share_pct REAL,
		source VARCHAR(32) NOT NULL DEFAULT '', conf REAL NOT NULL DEFAULT 0,
		origin VARCHAR(8) NOT NULL DEFAULT 'ocr', edited_by VARCHAR(64) NOT NULL DEFAULT '',
		edited_at DATETIME, extractor_version VARCHAR(16) NOT NULL DEFAULT '',
		created_at DATETIME, updated_at DATETIME,
		UNIQUE (report_date, run_date, dataset, scope, metric)
	);
`

func newSpotOriginInventoryTestDB(t *testing.T) context.Context {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spot-origin.sqlite3")
	if err := gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{{Type: "sqlite", Link: "sqlite::@file(" + path + ")"}},
	}); err != nil {
		t.Fatalf("设置数据库配置失败: %v", err)
	}
	t.Cleanup(func() { _ = gdb.SetConfig(gdb.Config{}) })

	ctx := context.Background()
	if _, err := g.DB().Exec(ctx, spotOriginInventoryTestDDL); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	return ctx
}

// seedSpotOriginRow 插一行，键由 (report_date, run_date, dataset, scope, metric) 唯一确定。
// origin 传 "" 表示老库那种空 origin 行（列是 NOT NULL，线上形态就是空串，
// 所有查询都带 IS NULL OR 兜底；统计必须能看见它而不是静默消失）。
func seedSpotOriginRow(t *testing.T, ctx context.Context, runDate, dataset, scope, metric string, value float64, origin any) {
	t.Helper()
	if _, err := g.DB().Exec(ctx, `
		INSERT INTO gd_spot_metric (report_date, run_date, dataset, scope, metric, value, unit, origin)
		VALUES (?, ?, ?, ?, ?, ?, '元/兆瓦时', ?)`,
		runDate, runDate, dataset, scope, metric, value, origin); err != nil {
		t.Fatalf("造行失败 (%s/%s/%s/%v): %v", dataset, scope, metric, origin, err)
	}
}

// 线上 11-14 的形状：pm-api 日频 + cv 预填档位 + manual 已确认档位 + void 作废旧值 + 老库 NULL 行。
func seedSpotOriginInventory(t *testing.T, ctx context.Context) {
	t.Helper()
	seedSpotOriginRow(t, ctx, "2026-11-10", "day_ahead_trade", "发电侧", "发电侧日前加权平均电价", 400, "pm-api")
	seedSpotOriginRow(t, ctx, "2026-11-11", "day_ahead_trade", "发电侧", "发电侧日前加权平均电价", 360, "pm-api")
	seedSpotOriginRow(t, ctx, "2026-11-11", "realtime_trade", "发电侧", "发电侧实时加权平均电价", 300, "pm-api")
	// 453 行 cv 档位的那一类：业务口径看不见，数据面必须看见
	seedSpotOriginRow(t, ctx, "2026-09-26", "node_price_peak", "广州", "价格档位", 4, "cv")
	seedSpotOriginRow(t, ctx, "2026-09-26", "node_price_valley", "广州", "价格档位", 1, "cv")
	seedSpotOriginRow(t, ctx, "2026-09-26", "node_price_peak", "韶关", "价格档位", 5, "cv")
	// 已人工确认的档位
	seedSpotOriginRow(t, ctx, "2026-09-22", "node_price_peak", "惠州", "价格档位", 6, "manual")
	// 被官方取代的旧值
	seedSpotOriginRow(t, ctx, "2026-09-16", "day_ahead_trade", "发电侧", "平均电价", 428.95, "void")
	// 老库空 origin —— 现有查询都带 IS NULL 兜底，统计也不能漏
	seedSpotOriginRow(t, ctx, "2026-09-16", "summary", "市场", "日前加权平均电价", 429, "")
}

// ① 数据面统计：合计 == 真实行数，且五类 origin（含空串）全部显形。
//
// 这一条直接钉住那个误判：如果 CountMetricsByOrigin 里有人加了白名单，
// 合计立刻小于真实行数，测试变红。
func TestCountMetricsByOriginCountsEveryRow(t *testing.T) {
	ctx := newSpotOriginInventoryTestDB(t)
	seedSpotOriginInventory(t, ctx)

	totalVal, err := g.DB().GetValue(ctx, `SELECT count(*) FROM gd_spot_metric`)
	if err != nil {
		t.Fatalf("查真实行数失败: %v", err)
	}
	total := totalVal.Int()

	got, err := GetSpotExtractService().CountMetricsByOrigin(ctx)
	if err != nil {
		t.Fatalf("CountMetricsByOrigin 失败: %v", err)
	}

	sum := 0
	for _, n := range got {
		sum += n
	}
	if sum != total {
		t.Fatalf("数据面统计合计 %d ≠ 真实行数 %d（有 origin 被白名单漏读）：%v", sum, total, got)
	}
	for origin, want := range map[string]int{
		s_db_model.SpotOriginPmAPI:  3,
		s_db_model.SpotOriginCV:     3,
		s_db_model.SpotOriginManual: 1,
		s_db_model.SpotOriginVoid:   1,
		"":                          1, // 老库空 origin
	} {
		if got[origin] != want {
			t.Fatalf("origin=%q 计数 %d，期望 %d（全部分组：%v）", origin, got[origin], want, got)
		}
	}
}

// ② 同一份数据：业务白名单写法（只看已确认）必须**小于**全量，差额恰好是未复核 + 作废 + 空 origin。
//
// 这条看着像废话，但它把两种口径的差写成可断言的等价关系：以后有人把业务白名单
// 替换进数据面统计（或反过来），两个数会相等，这里当场变红。
func TestOriginWhitelistAndInventoryAgreeOnWhatIsMissing(t *testing.T) {
	ctx := newSpotOriginInventoryTestDB(t)
	seedSpotOriginInventory(t, ctx)

	svc := GetSpotExtractService()

	inventory, err := svc.CountMetricsByOrigin(ctx)
	if err != nil {
		t.Fatalf("CountMetricsByOrigin 失败: %v", err)
	}
	inventoryTotal := 0
	for _, n := range inventory {
		inventoryTotal += n
	}

	// 业务口径：按白名单逐个数（等价于 ListMetricsByRunDateRange 的 WHERE origin IN (...)）
	trustedTotal := 0
	for _, origin := range s_db_model.SpotOriginsTrusted {
		trustedTotal += inventory[origin]
	}

	unreviewed := 0
	for _, origin := range s_db_model.SpotOriginsUnreviewed {
		unreviewed += inventory[origin]
	}
	voidTotal := inventory[s_db_model.SpotOriginVoid]
	legacyEmpty := inventory[""]

	if trustedTotal+unreviewed+voidTotal+legacyEmpty != inventoryTotal {
		t.Fatalf("三组口径 + 空 origin 未覆盖全量：trusted=%d unreviewed=%d void=%d empty=%d 合计 %d，全量 %d",
			trustedTotal, unreviewed, voidTotal, legacyEmpty,
			trustedTotal+unreviewed+voidTotal+legacyEmpty, inventoryTotal)
	}
	if trustedTotal >= inventoryTotal {
		t.Fatalf("业务白名单(%d) 与全量(%d) 相等 —— 说明这份数据造得没用，"+
			"或者有人把未复核行也塞进了 Trusted", trustedTotal, inventoryTotal)
	}
	// 差额必须能被未复核 + 作废 + NULL 解释，不是「凭空少了几行」
	if inventoryTotal-trustedTotal != unreviewed+voidTotal+legacyEmpty {
		t.Fatalf("白名单与全量的差额无法解释：全量 %d - trusted %d = %d，"+
			"但 unreviewed %d + void %d + empty %d = %d",
			inventoryTotal, trustedTotal, inventoryTotal-trustedTotal,
			unreviewed, voidTotal, legacyEmpty, unreviewed+voidTotal+legacyEmpty)
	}
}

// ③ 三组常量必须互斥、且并集覆盖全部已知 origin。
//
// 这是「新增 origin 时三组都要过一遍」的机器护栏：以后有人加一个 origin 只写进
// Trusted，这里的并集检查会放过它，但 ② 的差额检查会红 —— 两条一起才拦得住。
func TestSpotOriginSetsAreConsistent(t *testing.T) {
	seen := map[string]int{}
	for _, set := range [][]string{
		s_db_model.SpotOriginsTrusted,
		s_db_model.SpotOriginsUnreviewed,
		{s_db_model.SpotOriginVoid},
	} {
		for _, o := range set {
			seen[o]++
		}
	}
	for o, n := range seen {
		if n != 1 {
			t.Fatalf("origin %q 在口径分组里出现了 %d 次（必须刚好一次）", o, n)
		}
	}
	for _, o := range s_db_model.SpotOriginsInventory {
		if seen[o] != 1 {
			t.Fatalf("origin %q 在 SpotOriginsInventory 里但没进任何口径分组", o)
		}
	}
	for o := range seen {
		found := false
		for _, inv := range s_db_model.SpotOriginsInventory {
			if inv == o {
				found = true
			}
		}
		if !found {
			t.Fatalf("origin %q 进了口径分组但不在 SpotOriginsInventory 里（数据面统计会漏掉它）", o)
		}
	}
	if len(s_db_model.SpotOriginsInventory) != len(seen) {
		t.Fatalf("SpotOriginsInventory 有 %d 项，口径分组并集 %d 项 —— 两边必须一致",
			len(s_db_model.SpotOriginsInventory), len(seen))
	}
}
