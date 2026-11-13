package homemarket

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"s_db/s_db_model"
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
		// 编码必须由静态表映射出来，且与中文名一致 —— 空编码会撞唯一键，
		// 把 21 个城市挤成一行，而且写库不报错。
		if r.CityCode == "" {
			t.Errorf("cityCode 为空（中文名 %q 没映射到编码）: %+v", r.City, r)
		}
		if want, ok := CityCodeOf(r.City); !ok || want != r.CityCode {
			t.Errorf("cityCode = %q，与 CityCodeOf(%q) 不一致", r.CityCode, r.City)
		}
	}
	// 东莞 441900 / 广州 440100 / 深圳 440300
	want := map[string]string{"东莞市": "441900", "广州市": "440100", "深圳市": "440300"}
	for _, r := range rows {
		if w, ok := want[r.City]; ok && r.CityCode != w {
			t.Errorf("%s 的 cityCode = %q，期望 %q", r.City, r.CityCode, w)
		}
	}
}

// 源站若新增一个我们表里没有的市，必须跳过 + 告警，不能写空编码。
// 空编码会撞唯一键 (run_date, city_code, period_type)，21 个城市静默挤成一行。
func TestFetchNodePriceSkipsUnknownCity(t *testing.T) {
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"data":{"currentPirceAreaVOList":[
			{"name":"广州市","value":610.5,"type":1},
			{"name":"横琴新区","value":999.9,"type":1},
			{"name":"广州市","value":409.7,"type":2}]}}`)
	})

	rows, err := FetchNodePrice(context.Background(), "2026-11-13")
	if err != nil {
		t.Fatalf("FetchNodePrice 失败: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("条数 = %d，期望 2（未知地市被跳过）: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.City == "横琴新区" {
			t.Fatal("未知地市被写进结果了：编码为空会撞唯一键")
		}
		if r.CityCode == "" {
			t.Fatalf("写入了空 cityCode: %+v", r)
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

// 源站把字段改名后，返回 200 + 24 行结构完整但价格全 0。
// 只判 code/len 时这条路径根本不失败，0 元会被当成真实价格写进库，
// 页面显示「今天全天电价 0 元」。判据只能是「整批全为 0」。
func TestFetchHome24hAllZeroBatchIsParseError(t *testing.T) {
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"msg":"ok","data":{"details":[`)
		for h := 0; h < 24; h++ {
			if h > 0 {
				fmt.Fprint(w, ",")
			}
			// 字段名从 currentPrice 改名后，解码得到 0 值：结构合法、值是 0
			fmt.Fprintf(w, `{"powerNum":0,"currentPrice":0,"realTimePrice":0,"dateTime":"2026-11-07 %02d:00:00"}`, h)
		}
		fmt.Fprint(w, `]}}`)
	})

	rows, err := FetchHome24h(context.Background(), "2026-11-07")
	if err == nil {
		t.Fatalf("24 行全 0 应报错，实际返回 %d 行且 err=nil", len(rows))
	}
	if got := classifyErr(err); got != ErrKindParse {
		t.Errorf("错误类别 = %q，期望 %q（原文 %v）", got, ErrKindParse, err)
	}
	if !strings.Contains(err.Error(), "全为 0") {
		t.Errorf("错误文案要说清是「全为 0」而不是泛泛的解析失败：%v", err)
	}
}

func TestFetchNodePriceAllZeroBatchIsParseError(t *testing.T) {
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"data":{"currentPirceAreaVOList":[
			{"name":"广州市","value":0,"type":1},
			{"name":"广州市","value":0,"type":2},
			{"name":"深圳市","value":0,"type":1},
			{"name":"深圳市","value":0,"type":2}]}}`)
	})

	rows, err := FetchNodePrice(context.Background(), "2026-11-13")
	if err == nil {
		t.Fatalf("全 0 批次应报错，实际返回 %d 行且 err=nil", len(rows))
	}
	if got := classifyErr(err); got != ErrKindParse {
		t.Errorf("错误类别 = %q，期望 %q（原文 %v）", got, ErrKindParse, err)
	}
}

// 合法负电价必须照常写入。线上库实测 2026-11-08 湛江 period_type=2 是 -8.523，
// 用 <=0 做判据会把这一条误杀 —— 这条用例就是那条判据的守卫。
func TestFetchNodePriceKeepsLegalNegativePrice(t *testing.T) {
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"data":{"currentPirceAreaVOList":[
			{"name":"湛江市","value":-8.523,"type":2},
			{"name":"广州市","value":610.5,"type":1},
			{"name":"深圳市","value":0,"type":2}]}}`)
	})

	rows, err := FetchNodePrice(context.Background(), "2026-11-08")
	if err != nil {
		t.Fatalf("含合法负价 -8.523 的批次不该报错（说明判据误用了 <=0）：%v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("条数 = %d，期望 3", len(rows))
	}
	var got float64
	for _, r := range rows {
		if r.City == "湛江市" && r.PeriodType == 2 {
			got = r.Price
		}
	}
	if got != -8.523 {
		t.Errorf("湛江低谷价 = %v，期望 -8.523 原样保留", got)
	}
}

// 单点为 0 不算整批异常：只要批次里存在非 0 的有效值就照常写入。
func TestFetchHome24hSingleZeroPointIsNotRejected(t *testing.T) {
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"data":{"details":[
			{"powerNum":60000,"currentPrice":0,"realTimePrice":0,"dateTime":"2026-11-07 00:00:00"},
			{"powerNum":60000,"currentPrice":412.5,"realTimePrice":433.1,"dateTime":"2026-11-07 01:00:00"}]}}`)
	})

	rows, err := FetchHome24h(context.Background(), "2026-11-07")
	if err != nil {
		t.Fatalf("批次内有非 0 值就不该整批判失败：%v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("条数 = %d，期望 2", len(rows))
	}
}

// ---- 24 点两列独立判据（ahead / realtime 各判各的）----
//
// 旧判据是 `AheadPrice == 0 && RealtimePrice == 0`，必须两列同时为 0 才拦。
// 源站只把 currentPrice 改名（realTimePrice 不动）时，ahead 整批 0 而 realtime 正常，
// 守卫放行 → 0 元写库 → 页面画一条贴地的「日前电价」线，均值算成 0。
// 下面两条各钉一列，缺一条就会漏掉一半的改名形态。

// ahead 整批 0、realtime 全非 0：必须报 parse，且一条都不能返回。
// 夹具里 realtime 带 610.5 这类真实非零值（不是 0），旧判据在这条上是放行的。
func TestFetchHome24hAheadAllZeroRealtimeNormalIsParseError(t *testing.T) {
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"msg":"ok","data":{"details":[`)
		for h := 0; h < 24; h++ {
			if h > 0 {
				fmt.Fprint(w, ",")
			}
			// currentPrice 改名 → 解出 0；realTimePrice 照常
			fmt.Fprintf(w, `{"powerNum":%d,"currentPrice":0,"realTimePrice":%d.5,"dateTime":"2026-11-07 %02d:00:00"}`,
				60000+h, 610+h, h)
		}
		fmt.Fprint(w, `]}}`)
	})

	rows, err := FetchHome24h(context.Background(), "2026-11-07")
	if err == nil {
		t.Fatalf("ahead 整批为 0 应报错，实际返回 %d 行且 err=nil（0 元会被写成真实日前价）", len(rows))
	}
	if got := classifyErr(err); got != ErrKindParse {
		t.Errorf("错误类别 = %q，期望 %q（原文 %v）", got, ErrKindParse, err)
	}
	if rows != nil {
		t.Errorf("报错时不应返回任何行，实际 %d 行", len(rows))
	}
}

// realtime 整批 0、ahead 全非 0：同样必须报 parse。
// 这条防的是「只修 ahead 那一半」—— 反向改名一样会把 0 写进 realtime_price。
func TestFetchHome24hRealtimeAllZeroAheadNormalIsParseError(t *testing.T) {
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"msg":"ok","data":{"details":[`)
		for h := 0; h < 24; h++ {
			if h > 0 {
				fmt.Fprint(w, ",")
			}
			// realTimePrice 改名 → 解出 0；currentPrice 照常
			fmt.Fprintf(w, `{"powerNum":%d,"currentPrice":%d.5,"realTimePrice":0,"dateTime":"2026-11-07 %02d:00:00"}`,
				60000+h, 400+h, h)
		}
		fmt.Fprint(w, `]}}`)
	})

	rows, err := FetchHome24h(context.Background(), "2026-11-07")
	if err == nil {
		t.Fatalf("realtime 整批为 0 应报错，实际返回 %d 行且 err=nil", len(rows))
	}
	if got := classifyErr(err); got != ErrKindParse {
		t.Errorf("错误类别 = %q，期望 %q（原文 %v）", got, ErrKindParse, err)
	}
	if rows != nil {
		t.Errorf("报错时不应返回任何行，实际 %d 行", len(rows))
	}
}

// 判据必须「整批全为 0」而不是 `<=0`：负价（含合法负电价）与非 0 值混在一起时照常放行。
// 这条是变异守卫 —— 把任一列判据改成 <=0，本用例立刻变红。
// 线上库实测：gd_home_24h 192 行最小 53.2；gd_node_price 有 -8.523 的合法负价。
func TestFetchHome24hKeepsNegativeAndMixedValues(t *testing.T) {
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"msg":"ok","data":{"details":[
			{"powerNum":60000,"currentPrice":-8.523,"realTimePrice":-3.2,"dateTime":"2026-11-07 00:00:00"},
			{"powerNum":60000,"currentPrice":412.5,"realTimePrice":0,"dateTime":"2026-11-07 01:00:00"},
			{"powerNum":60000,"currentPrice":0,"realTimePrice":433.1,"dateTime":"2026-11-07 02:00:00"}]}}`)
	})

	rows, err := FetchHome24h(context.Background(), "2026-11-07")
	if err != nil {
		t.Fatalf("含负价/非 0 值的批次不该被判失败：%v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("条数 = %d，期望 3", len(rows))
	}
	if rows[0].AheadPrice != -8.523 || rows[0].RealtimePrice != -3.2 {
		t.Errorf("合法负价被改写：%+v", rows[0])
	}
}

// 整批负价是合法数据，不是「字段改名」。
//
// 这条是 `<=0` 变异的唯一杀手：guardBatch 只要遇到一个非 0 值就放行，所以
// 「混入一个 610.5」的夹具对 ==0 和 <=0 完全等价 —— 只有**整批 ≤0 且含负值**能区分。
// 深度供大于求时全天负价在理论上是可能形态，此时必须原样写入而不是判成「全 0 拒绝」。
func TestFetchHome24hAllNegativeBatchIsNotRejected(t *testing.T) {
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"msg":"ok","data":{"details":[`)
		for h := 0; h < 24; h++ {
			if h > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{"powerNum":%d,"currentPrice":%d.5,"realTimePrice":%d.25,"dateTime":"2026-11-07 %02d:00:00"}`,
				60000+h, -(h+1), -(h+2), h)
		}
		fmt.Fprint(w, `]}}`)
	})

	rows, err := FetchHome24h(context.Background(), "2026-11-07")
	if err != nil {
		t.Fatalf("整批负价是合法数据，不该被判成「全为 0」：%v", err)
	}
	if len(rows) != 24 {
		t.Fatalf("条数 = %d，期望 24", len(rows))
	}
	if rows[0].AheadPrice != -1.5 || rows[0].RealtimePrice != -2.25 {
		t.Errorf("负价被改写：%+v", rows[0])
	}
}

// 地市同样：整批负价照常写入（线上 2026-11-08 湛江低谷已有 -8.523 先例）。
func TestFetchNodePriceAllNegativeBatchIsNotRejected(t *testing.T) {
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"data":{"currentPirceAreaVOList":[
			{"name":"湛江市","value":-8.523,"type":2},
			{"name":"广州市","value":-1.2,"type":2},
			{"name":"深圳市","value":-0.4,"type":2}]}}`)
	})

	rows, err := FetchNodePrice(context.Background(), "2026-11-08")
	if err != nil {
		t.Fatalf("整批负价不该被判成「全为 0」：%v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("条数 = %d，期望 3", len(rows))
	}
}
// ⑥ 只有 ahead 整批 0（realtime 正常）：run 表记 failed/parse、数据表一行不多。
// 旧判据 `ahead==0 && realtime==0` 在这条上是放行的，0 元会写进 gd_home_24h 的 ahead_price，
// 页面 24 点电价图直接把「日前」那条线画成贴地 0 元、MeanAhead 算成 0。
func TestCollectAheadOnlyZeroBatchWritesNoData(t *testing.T) {
	newTestDB(t)
	withStub(t, func(w http.ResponseWriter, r *http.Request) {
		date := r.URL.Query().Get("date")
		fmt.Fprint(w, `{"code":200,"msg":"ok","data":{"details":[`)
		for h := 0; h < 24; h++ {
			if h > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{"powerNum":%d,"currentPrice":0,"realTimePrice":%d.5,"dateTime":"%s %02d:00:00"}`,
				60000+h, 610+h, date, h)
		}
		fmt.Fprint(w, `]}}`)
	})

	if _, _, err := CollectHome24h(context.Background(), time.Now()); err == nil {
		t.Fatal("ahead 整批 0 应返回 error（否则 0 元会被写成真实日前价）")
	} else if got := classifyErr(err); got != ErrKindParse {
		t.Errorf("错误类别 = %q，期望 %q（原文 %v）", got, ErrKindParse, err)
	}
	if got := countRows(t, s_db_model.TableHome24h); got != 0 {
		t.Fatalf("ahead 整批 0 却往数据表写了 %d 行，期望 0（页面会显示日前电价全天 0 元）", got)
	}
	runRows := fetchRunRows(t, s_db_model.DatasetHome24h)
	if len(runRows) == 0 {
		t.Fatal("run 表一行都没写，排障时看不出发生过什么")
	}
	for _, r := range runRows {
		if r.Status != s_db_model.FetchStatusFailed || r.ErrorKind != string(ErrKindParse) {
			t.Errorf("%s run 行 = %+v，期望 failed/parse", r.TargetDate, r)
		}
		if r.RowsWritten != 0 {
			t.Errorf("%s 记了 %d 行写入，期望 0", r.TargetDate, r.RowsWritten)
		}
	}
}
