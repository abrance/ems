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

	// 窗口取 9 天（> 滞后 6 天），这样 24 点应有 3 天可回补：T-8 / T-7 / T-6。
	// 若首次启动只跑 CollectHome24h，只会写进 T-6 一天 —— 那就是这次要守的缺口。
	const days = 9
	now := time.Now()
	runOnce(context.Background(), days, true)

	got24h := distinctDates(t, `SELECT DISTINCT price_date AS d FROM gd_home_24h ORDER BY price_date`)
	want24h := []string{
		now.AddDate(0, 0, -8).Format(dateLayout),
		now.AddDate(0, 0, -7).Format(dateLayout),
		now.AddDate(0, 0, -6).Format(dateLayout),
	}
	if len(got24h) != len(want24h) {
		t.Fatalf("24 点回补天数 = %d %v，期望 %d 天 %v（首次启动必须整窗回补，不能只取最近有数的一天）",
			len(got24h), got24h, len(want24h), want24h)
	}
	for i := range want24h {
		if got24h[i] != want24h[i] {
			t.Errorf("24 点第 %d 天 = %s，期望 %s", i, got24h[i], want24h[i])
		}
	}
	// 每天的 24 个整点都要齐（回补是按日取整批，不是只写一个点）
	if n := len(distinctDates(t, `SELECT DISTINCT hour AS d FROM gd_home_24h WHERE price_date = '`+want24h[0]+`'`)); n != 24 {
		t.Errorf("%s 的点数 = %d，期望 24", want24h[0], n)
	}

	gotNode := distinctDates(t, `SELECT DISTINCT run_date AS d FROM gd_node_price ORDER BY run_date`)
	if len(gotNode) != days {
		t.Fatalf("地市回补天数 = %d %v，期望 %d 天（含当天）", len(gotNode), gotNode, days)
	}
}

func TestLaterRunsDoNotRescanWindow(t *testing.T) {
	newTestDB(t)
	newLagStub(t)

	// 非首次：24 点只采最近有数的一天、地市只采当天。
	// 守住 P2 —— 别把每 30 分钟一轮都变成整窗重扫（源站有动态防护）。
	const days = 9
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
	home24h, node := RunBackfill(context.Background(), now, 999)

	if len(node) != MaxBackfillDays {
		t.Errorf("地市回补天数 = %d，期望夹到上限 %d", len(node), MaxBackfillDays)
	}
	// 24 点只有滞后 6 天以后的日期才有数，30 天窗口里应得 24 天
	if want := MaxBackfillDays - stubLagDays; len(home24h) != want {
		t.Errorf("24 点回补天数 = %d，期望 %d（窗口 %d 天减去滞后 %d 天）",
			len(home24h), want, MaxBackfillDays, stubLagDays)
	}
	for date, n := range home24h {
		if n != 24 {
			t.Errorf("%s 写入 %d 点，期望 24", date, n)
		}
	}
}
