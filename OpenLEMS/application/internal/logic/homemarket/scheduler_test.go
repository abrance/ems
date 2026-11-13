package homemarket

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// 调度层单测：守「首次启动两路都回补」这一条。
//
// 背景（#143 后的缺口）：24 点只靠 CollectHome24h，它探到最近一个有数的一天就停手；
// 进程停机跨过一个发布日，恢复后中间几天永久缺。首次启动必须按窗口回补。
// 用 http 桩 + 临时 sqlite，不打真源站。
//
// 桩模拟真实滞后口径：24 点 T-6 才有数，地市 T+0。
const stubLagDays = 6

// newLagStub 返回一个主页网关桩：24 点只发布 today-lag 及更早，地市每天都发布。
func newLagStub(t *testing.T) {
	t.Helper()
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		date := r.URL.Query().Get("date")
		d, err := time.ParseInLocation(dateLayout, date, time.Local)
		if err != nil {
			http.Error(w, "bad date", http.StatusBadRequest)
			return
		}
		daysAgo := int(time.Since(d).Hours() / 24)
		switch {
		case strings.HasSuffix(r.URL.Path, "/trade/tradeSituation"):
			if daysAgo < stubLagDays {
				// 未发布：源站返回 200 + details=[]
				fmt.Fprint(w, `{"code":200,"msg":"操作成功！","data":{"details":[]}}`)
				return
			}
			fmt.Fprint(w, `{"code":200,"msg":"ok","data":{"details":[`)
			for h := 0; h < 24; h++ {
				if h > 0 {
					fmt.Fprint(w, ",")
				}
				fmt.Fprintf(w, `{"powerNum":%d,"currentPrice":%d,"realTimePrice":%d,"dateTime":"%s %02d:00:00"}`,
					60000+h, 400+h, 500+h, date, h)
			}
			fmt.Fprint(w, `]}}`)
		case strings.HasSuffix(r.URL.Path, "/marketEntitiesNum"):
			fmt.Fprint(w, `{"code":200,"data":{"currentPirceAreaVOList":[`)
			fmt.Fprintf(w, `{"name":"广州市","value":610.5,"type":1},`)
			fmt.Fprintf(w, `{"name":"广州市","value":409.7,"type":2},`)
			fmt.Fprintf(w, `{"name":"深圳市","value":611.0,"type":1},`)
			fmt.Fprintf(w, `{"name":"深圳市","value":410.0,"type":2}`)
			fmt.Fprint(w, `]}}`)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	})
}

// distinctDates 读某表的日期列去重（真库，验证落库而不是只看函数返回值）。
func distinctDates(t *testing.T, sqlText string) []string {
	t.Helper()
	var rows []struct {
		D string `orm:"d"`
	}
	if err := g.DB().GetScan(context.Background(), &rows, sqlText); err != nil {
		t.Fatalf("查日期失败: %v", err)
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.D)
	}
	return out
}

func TestFirstRunBackfillsHome24hAndNodePrice(t *testing.T) {
	newTestDB(t)
	newLagStub(t)

	// 窗口用**生产默认配置值**（config.yaml 里的 home_market.backfill_days: 7），
	// 不手写一个好看的数字 —— 之前测试写 9（> 滞后 6）所以绿了，而生产 7 天窗口
	// 里只有 T-6 一天有数，停机 5 天重启仍然缺 4 天。
	days := ParseDays(g.Cfg().MustGet(context.Background(), "home_market.backfill_days", "7").String())
	if days != 7 {
		t.Fatalf("测试假设配置回补天数 = 7（生产默认），实际读到 %d", days)
	}

	now := time.Now()
	runOnce(context.Background(), days, true)

	got24h := distinctDates(t, `SELECT DISTINCT price_date AS d FROM gd_home_24h ORDER BY price_date`)
	// 24 点窗口与地市解耦：7 天窗口扣掉 6 天滞后只剩 1 天，所以窗口必须比 days 大。
	// 下界 = maxLookbackDays - 滞后；上界 = 地市窗口（24 点不能比地市窗口小）。
	wantMin := Home24hWindowDays(days) - stubLagDays
	if len(got24h) < wantMin {
		t.Fatalf("24 点回补天数 = %d %v，期望 ≥ %d（窗口 %d 天减去滞后 %d 天）；"+
			"只得到 1 天说明两路窗口被塞进同一个数字里了",
			len(got24h), got24h, wantMin, Home24hWindowDays(days), stubLagDays)
	}
	if len(got24h) <= 1 {
		t.Fatalf("24 点回补只有 %v 一天：默认配置下这不叫整窗回补", got24h)
	}
	// 最后一天必须是 T-滞后（24 点能取到的最新一天），往前连续不缺
	newest := now.AddDate(0, 0, -stubLagDays).Format(dateLayout)
	if got24h[len(got24h)-1] != newest {
		t.Errorf("24 点最新一天 = %s，期望 %s", got24h[len(got24h)-1], newest)
	}
	for i := 1; i < len(got24h); i++ {
		prev, err := time.ParseInLocation(dateLayout, got24h[i-1], time.Local)
		if err != nil {
			t.Fatalf("解析日期 %s 失败: %v", got24h[i-1], err)
		}
		if want := prev.AddDate(0, 0, 1).Format(dateLayout); got24h[i] != want {
			t.Errorf("24 点回补不连续：%s 之后是 %s，期望 %s", got24h[i-1], got24h[i], want)
		}
	}
	// 每天的 24 个整点都要齐（回补是按日取整批，不是只写一个点）
	if n := len(distinctDates(t, `SELECT DISTINCT hour AS d FROM gd_home_24h WHERE price_date = '`+got24h[0]+`'`)); n != 24 {
		t.Errorf("%s 的点数 = %d，期望 24", got24h[0], n)
	}

	gotNode := distinctDates(t, `SELECT DISTINCT run_date AS d FROM gd_node_price ORDER BY run_date`)
	if len(gotNode) != days {
		t.Fatalf("地市回补天数 = %d %v，期望 %d 天（含当天，T+0 口径不受 24 点滞后影响）", len(gotNode), gotNode, days)
	}
}

// TestHome24hWindowDecoupledFromNodePrice 守「调用方无法再把两个不同滞后的口径
// 塞进同一个数字里」：手动接口传 7（生产默认）时，24 点窗口不能被 7 截住。
func TestHome24hWindowDecoupledFromNodePrice(t *testing.T) {
	newTestDB(t)
	newLagStub(t)

	const days = defaultBackfillDays // 生产默认 7
	now := time.Now()
	home24h, node, err := RunBackfill(context.Background(), now, days)
	if err != nil {
		t.Fatalf("RunBackfill 失败: %v", err)
	}

	if len(node) != days {
		t.Errorf("地市回补天数 = %d，期望 %d（地市窗口就是调用方给的那个数字）", len(node), days)
	}
	if len(home24h) != Home24hWindowDays(days)-stubLagDays {
		t.Errorf("24 点回补天数 = %d %v，期望 %d（窗口 %d - 滞后 %d）",
			len(home24h), home24h, Home24hWindowDays(days)-stubLagDays, Home24hWindowDays(days), stubLagDays)
	}
	if len(home24h) <= 1 {
		t.Errorf("24 点只回补了 %d 天：窗口按地市的 7 天算就会正好剩 T-6 一天", len(home24h))
	}
}

func TestHome24hWindowDays(t *testing.T) {
	// 默认值 / 小于 maxLookbackDays 都提到 14；大于则跟随；再大就夹到 30。
	cases := []struct{ in, want int }{
		{0, maxLookbackDays},
		{1, maxLookbackDays},
		{defaultBackfillDays, maxLookbackDays},
		{maxLookbackDays, maxLookbackDays},
		{20, 20},
		{MaxBackfillDays, MaxBackfillDays},
		{999, MaxBackfillDays},
	}
	for _, c := range cases {
		if got := Home24hWindowDays(c.in); got != c.want {
			t.Errorf("Home24hWindowDays(%d) = %d，期望 %d", c.in, got, c.want)
		}
	}
}

func TestLaterRunsDoNotRescanWindow(t *testing.T) {
	newTestDB(t)
	newLagStub(t)

	// 非首次：24 点只采最近有数的一天、地市只采当天。
	// 守住 P2 —— 别把每 30 分钟一轮都变成整窗重扫（源站有动态防护）。
	days := defaultBackfillDays
	now := time.Now()
	runOnce(context.Background(), days, false)

	got24h := distinctDates(t, `SELECT DISTINCT price_date AS d FROM gd_home_24h`)
	if len(got24h) != 1 || got24h[0] != now.AddDate(0, 0, -stubLagDays).Format(dateLayout) {
		t.Fatalf("24 点常规轮询落库 = %v，期望只有 %s 一天",
			got24h, now.AddDate(0, 0, -stubLagDays).Format(dateLayout))
	}

	gotNode := distinctDates(t, `SELECT DISTINCT run_date AS d FROM gd_node_price`)
	if len(gotNode) != 1 || gotNode[0] != now.Format(dateLayout) {
		t.Fatalf("地市常规轮询落库 = %v，期望只有当天 %s", gotNode, now.Format(dateLayout))
	}
}

func TestRunBackfillClampsWindowAndCoversBothRoutes(t *testing.T) {
	newTestDB(t)
	newLagStub(t)

	// 手动补洞入口：窗口上限 30 天，两路都回补。
	// 传 999 天若不夹紧，会对着有动态防护的源站连打 999 次。
	now := time.Now()
	home24h, node, err := RunBackfill(context.Background(), now, 999)
	if err != nil {
		t.Fatalf("RunBackfill 失败: %v", err)
	}

	if len(node) != MaxBackfillDays {
		t.Errorf("地市回补天数 = %d，期望夹到上限 %d", len(node), MaxBackfillDays)
	}
	// 24 点窗口同样夹到上限，只有滞后 6 天以后的日期才有数
	if want := Home24hWindowDays(MaxBackfillDays) - stubLagDays; len(home24h) != want {
		t.Errorf("24 点回补天数 = %d，期望 %d（窗口 %d 天减去滞后 %d 天）",
			len(home24h), want, Home24hWindowDays(MaxBackfillDays), stubLagDays)
	}
	for date, n := range home24h {
		if n != 24 {
			t.Errorf("%s 写入 %d 点，期望 24", date, n)
		}
	}
}
