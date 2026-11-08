//go:build integration

// IT-PRICE-01：电价写入 → 落库 → 缓存加载 → 按时间取电价时段。
// 场景说明见同目录 price-sync-to-api.md；规范见 docs/testing-standard.md。
package test_plan

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"common/c_enum"
	"s_db"
	"s_db/s_db_model"
	"s_price"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"
)

// TestMain 准备一个独立的库：优先用 TEST_DB_* 指定的 sqlite，否则用临时目录里的库。
// 不用外部中间件启动器，也不复用 ./out/db.sqlite3，避免污染开发数据。
func TestMain(m *testing.M) {
	if env := os.Getenv("TEST_ENV"); env == "prod" || env == "production" {
		log.Fatalf("TEST_ENV=%s：集成测试不允许指向生产环境", env)
	}
	if d := os.Getenv("TEST_DB_DRIVER"); d != "" && d != "sqlite" {
		log.Fatalf("IT-PRICE-01 目前只实现 sqlite（与生产同款内嵌库），收到 TEST_DB_DRIVER=%s", d)
	}

	dir := os.Getenv("TEST_ARTIFACT_DIR")
	if dir == "" {
		var err error
		if dir, err = os.MkdirTemp("", "it-price-01-"); err != nil {
			log.Fatalf("创建临时目录失败: %v", err)
		}
		defer os.RemoveAll(dir)
	}
	// 库文件默认落在本次运行的临时/产物目录；TEST_DB_DSN 给了就用它。
	// 路径必须先转绝对：下面要 chdir，而 sqlite 链接串按进程工作目录解析。
	dbFile := filepath.Join(dir, "it-price-01.sqlite3")
	if dsn := os.Getenv("TEST_DB_DSN"); dsn != "" {
		if _, after, ok := strings.Cut(dsn, "sqlite::@file("); ok {
			dsn = strings.TrimSuffix(after, ")")
		}
		abs, err := filepath.Abs(dsn)
		if err != nil {
			log.Fatalf("解析 TEST_DB_DSN 失败: %v", err)
		}
		dbFile = abs
	}
	log.Printf("集成测试库: %s", dbFile)
	if err := os.MkdirAll(filepath.Dir(dbFile), 0o755); err != nil {
		log.Fatalf("创建数据库目录失败: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatalf("创建目录失败: %v", err)
	}
	// GoFrame 从工作目录的 config.yaml 读 database.default.link
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(fmt.Sprintf(`database:
  default:
    link: "sqlite::@file(%s)"
    timezone: "Local"
log:
  level: "warn"
`, dbFile)), 0o644); err != nil {
		log.Fatalf("写测试配置失败: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		log.Fatalf("切换工作目录失败: %v", err)
	}

	if err := s_db.Init(); err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	os.Exit(m.Run())
}

func TestIT_PRICE_01_PriceRoundTrip(t *testing.T) {
	ctx := gctx.New()
	svc := s_db.GetPriceService()

	// 场景互相独立：先清掉库里已有电价，结果才有确定性
	if _, err := g.Model(s_db_model.TablePrice).Ctx(ctx).Where("1=1").Delete(); err != nil {
		t.Fatalf("清理电价表失败: %v", err)
	}

	// 一条长期有效、覆盖全天五个电价类型的电价（值取自广东分时电价的量级）
	segments := `[` +
		`{"startTime":"00:00","endTime":"08:00","priceType":"Valley","price":0.32},` +
		`{"startTime":"08:00","endTime":"12:00","priceType":"Flat","price":0.68},` +
		`{"startTime":"12:00","endTime":"18:00","priceType":"Peak","price":1.12},` +
		`{"startTime":"18:00","endTime":"21:00","priceType":"Sharp","price":1.45},` +
		`{"startTime":"21:00","endTime":"23:59","priceType":"DeepValley","price":0.19}` +
		`]`

	model := &s_db_model.SPriceModel{
		Description:   "IT-PRICE-01 分时电价",
		Priority:      1,
		Status:        "Enable",
		DateRange:     `{"isLongTerm":true}`,
		TimeRange:     `{}`,
		PriceSegments: segments,
		RemoteId:      "it-price-01",
	}
	id, err := svc.CreatePrice(ctx, model)
	if err != nil {
		t.Fatalf("写入电价失败: %v", err)
	}
	t.Cleanup(func() {
		if err := svc.DeletePrice(context.Background(), id); err != nil {
			t.Logf("清理电价 id=%d 失败: %v", id, err)
		}
	})
	if id <= 0 {
		t.Fatalf("写入电价后没有拿到自增 ID: %d", id)
	}

	// 落库后原文必须是枚举名，而不是 string(枚举) 那种单字符控制符
	stored, err := svc.GetPriceById(ctx, id)
	if err != nil {
		t.Fatalf("读回电价失败: %v", err)
	}
	if strings.ContainsRune(stored.PriceSegments, '\x01') {
		t.Fatalf("电价类型被写成了控制字符，消费侧会全部解析成平电: %q", stored.PriceSegments)
	}

	// 缓存加载走的是生产同一条路径：DB → s_db → 解析 priceType → 时段
	if err := s_price.RefreshPriceCache(ctx); err != nil {
		t.Fatalf("刷新电价缓存失败: %v", err)
	}

	cases := []struct {
		clock string
		want  c_enum.EPriceType
		price float64
	}{
		{"01:00", c_enum.EPriceTypeValley, 0.32},
		{"09:00", c_enum.EPriceTypeFlat, 0.68},
		{"14:00", c_enum.EPriceTypePeak, 1.12},
		{"19:00", c_enum.EPriceTypeSharp, 1.45},
		{"22:00", c_enum.EPriceTypeDeepValley, 0.19},
	}
	for _, c := range cases {
		at := atClock(t, c.clock)
		seg, err := s_price.PriceAt(ctx, at)
		if err != nil {
			t.Fatalf("%s 取电价时段失败: %v", c.clock, err)
		}
		if seg == nil {
			t.Fatalf("%s 没有匹配到电价时段", c.clock)
		}
		if seg.PriceType != c.want {
			t.Errorf("%s 电价类型 = %v，期望 %v（落库/解析不互逆）", c.clock, seg.PriceType, c.want)
		}
		if seg.Price != c.price {
			t.Errorf("%s 电价 = %v，期望 %v", c.clock, seg.Price, c.price)
		}
	}

	// 当前激活电价优先级与描述要能读到
	active, err := s_price.GetCurrentActivePrice(ctx)
	if err != nil {
		t.Fatalf("获取激活电价失败: %v", err)
	}
	if active == nil || active.Priority != 1 || active.Description != model.Description {
		t.Fatalf("激活电价不符: %+v", active)
	}
}

// atClock 返回今天指定 HH:MM 的时间点，时段判断只比较时分。
func atClock(t *testing.T, clock string) time.Time {
	t.Helper()
	hm, err := time.Parse("15:04", clock)
	if err != nil {
		t.Fatalf("非法时间 %s: %v", clock, err)
	}
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), hm.Hour(), hm.Minute(), 0, 0, now.Location())
}
