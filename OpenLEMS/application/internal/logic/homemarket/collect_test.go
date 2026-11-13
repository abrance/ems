package homemarket

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 解析层单测：拿本地桩服务替掉真实源站。
//
// 守的是三件容易错的事：
//  1. 源站把业务错误写在 code 里（HTTP 200），只看状态码会把「没数据」当成功；
//  2. dateTime 是 "YYYY-MM-DD HH:MM:SS"，hour 必须取整点而不是字符串切错位；
//  3. 地市只有 type=1/2 两个时段，出现别的值要跳过而不是塞进表里。
func withStub(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	old := gatewayBase
	gatewayBase = srv.URL
	t.Cleanup(func() {
		gatewayBase = old
		srv.Close()
	})
}

func TestFetchHome24hParses24Points(t *testing.T) {
	var body strings.Builder
	body.WriteString(`{"code":200,"msg":"ok","data":{"details":[`)
	for h := 0; h < 24; h++ {
		if h > 0 {
			body.WriteString(",")
		}
		fmt.Fprintf(&body, `{"powerNum":%d.5,"currentPrice":%d,"realTimePrice":%d,"dateTime":"2026-11-07 %02d:00:00"}`,
			60000+h, 400+h, 500+h, h)
	}
	body.WriteString(`]}}`)

	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("date"); got != "2026-11-07" {
			t.Errorf("date 参数 = %q，期望 2026-11-07", got)
		}
		if got := r.URL.Query().Get("type"); got != "1" {
			t.Errorf("type 参数 = %q，期望 1", got)
		}
		if !strings.HasSuffix(r.URL.Path, "/trade/tradeSituation") {
			t.Errorf("path = %q，期望以 /trade/tradeSituation 结尾", r.URL.Path)
		}
		fmt.Fprint(w, body.String())
	})

	rows, err := FetchHome24h(context.Background(), "2026-11-07")
	if err != nil {
		t.Fatalf("FetchHome24h 失败: %v", err)
	}
	if len(rows) != 24 {
		t.Fatalf("点数 = %d，期望 24", len(rows))
	}
	for i, r := range rows {
		if r.Hour != i {
			t.Errorf("第 %d 条 hour = %d，期望 %d", i, r.Hour, i)
		}
		if r.PriceDate != "2026-11-07" {
			t.Errorf("price_date = %q，期望 2026-11-07", r.PriceDate)
		}
		if r.Source != SourceHome24h {
			t.Errorf("source = %q，期望 %q", r.Source, SourceHome24h)
		}
	}
	if rows[0].AheadPrice != 400 || rows[0].RealtimePrice != 500 || rows[0].PowerMw != 60000.5 {
		t.Errorf("首点字段解析错：%+v", rows[0])
	}
}

func TestFetchHome24hEmptyDayIsNotError(t *testing.T) {
	// 实测：11-08 起主页返回 200 + details=[]（还没发布）。
	// 这是「这天没数」，不是错误 —— 采到错误会让调度器以为源站挂了。
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"msg":"操作成功！","data":{"details":[]}}`)
	})
	rows, err := FetchHome24h(context.Background(), "2026-11-08")
	if err != nil {
		t.Fatalf("空数据不该报错，实际: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("点数 = %d，期望 0", len(rows))
	}
}

func TestFetchHome24hSurfacesBusinessCode(t *testing.T) {
	// HTTP 200 但 code != 200：必须报错，不能当成「今天没数据」静默跳过
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":500,"msg":"系统繁忙","data":null}`)
	})
	if _, err := FetchHome24h(context.Background(), "2026-11-07"); err == nil {
		t.Fatal("code=500 时应返回错误")
	} else if !strings.Contains(err.Error(), "500") {
		t.Errorf("错误里要带源站 code，实际: %v", err)
	}
}

func TestFetchNodePriceParses42RowsAndSkipsUnknownType(t *testing.T) {
	cities := []string{"东莞市", "广州市", "深圳市"}
	var items []string
	for _, c := range cities {
		items = append(items, fmt.Sprintf(`{"name":%q,"value":610.5,"type":1}`, c))
		items = append(items, fmt.Sprintf(`{"name":%q,"value":409.7,"type":2}`, c))
	}
	// 未知时段：源站若新增 type=3，跳过它而不是写进表（表里只定义 1/2）
	items = append(items, `{"name":"测试市","value":1.0,"type":3}`)

	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("model"); got != "2" {
			t.Errorf("model 参数 = %q，期望 2", got)
		}
		fmt.Fprintf(w, `{"code":200,"data":{"currentPirceAreaVOList":[%s]}}`, strings.Join(items, ","))
	})

	rows, err := FetchNodePrice(context.Background(), "2026-11-13")
	if err != nil {
		t.Fatalf("FetchNodePrice 失败: %v", err)
	}
	if len(rows) != 6 {
		t.Fatalf("条数 = %d，期望 6（3 市 × 2 时段，type=3 跳过）", len(rows))
	}
	for _, r := range rows {
		if r.PeriodType != 1 && r.PeriodType != 2 {
			t.Errorf("写入了未知时段: %+v", r)
		}
		if r.Source != SourceNodePrice {
			t.Errorf("source = %q，期望 %q", r.Source, SourceNodePrice)
		}
	}
}

func TestHourOfRejectsBadFormat(t *testing.T) {
	if _, err := hourOf("2026-11-07T00:00:00"); err == nil {
		t.Error("ISO 格式应被拒绝（源站是空格分隔），否则会静默错位")
	}
	h, err := hourOf("2026-11-07 23:00:00")
	if err != nil || h != 23 {
		t.Errorf("hourOf = %d, %v；期望 23, nil", h, err)
	}
}

// 14 天全失败：必须返回带类别的 error。
// 返回 nil 会被调度器写成「最近 14 天都没有数据（源站未发布）」，源站故障被记成没发布，
// 建在这句日志上的告警全是假的。
func TestCollectHome24hAllFailuresSurfaceClassifiedError(t *testing.T) {
	newTestDB(t) // 现在每次尝试都会写 run 表
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusBadRequest) // 动态防护的典型形态
	})

	date, n, err := CollectHome24h(context.Background(), time.Now())
	if err == nil {
		t.Fatal("14 天全部 400 时应返回 error（否则调度器打成「源站未发布」）")
	}
	if date != "" || n != 0 {
		t.Errorf("失败时不该报日期/点数：date=%q n=%d", date, n)
	}
	if got := classifyErr(err); got != ErrKindBlocked {
		t.Errorf("错误类别 = %q，期望 %q（原文 %v）", got, ErrKindBlocked, err)
	}
	if !strings.Contains(err.Error(), string(ErrKindBlocked)) {
		t.Errorf("错误文案该带类别：%v", err)
	}
}

// 请求成功且 payload 明确为空：这才是「源站未发布」，返回 nil 且不报错。
func TestCollectHome24hEmptyPayloadIsNotError(t *testing.T) {
	newTestDB(t)
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"msg":"操作成功！","data":{"details":[]}}`)
	})

	date, n, err := CollectHome24h(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("空 payload 不该报错，实际: %v", err)
	}
	if date != "" || n != 0 {
		t.Errorf("未发布时应返回空日期/0 点：date=%q n=%d", date, n)
	}
}

// 错误归类：JSON 形状不符要归 parse，别和网络/拦截混在一起。
func TestGetJSONClassifiesParseError(t *testing.T) {
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not json`)
	})
	var out tradeSituationResp
	err := getJSON(context.Background(), gatewayBase+"/x", &out)
	if err == nil {
		t.Fatal("非 JSON 应返回错误")
	}
	if got := classifyErr(err); got != ErrKindParse {
		t.Errorf("错误类别 = %q，期望 %q（原文 %v）", got, ErrKindParse, err)
	}
}
