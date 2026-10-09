package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 多变量输入：values 要按二维发出去，服务端才能把额外通道当协变量。
func TestSeriesMarshalMatrix(t *testing.T) {
	raw, err := json.Marshal(&SSeries{
		Id:     "real",
		Matrix: [][]float64{{1, 2, 3}, {10, 20, 30}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"real","values":[[1,2,3],[10,20,30]]}`
	if string(raw) != want {
		t.Fatalf("多变量请求体 = %s, want %s", raw, want)
	}

	// 单变量还是老样子（一维数组），不要因为加了多变量支持就把老调用改形状
	raw, err = json.Marshal(&SSeries{Id: "load", Values: []float64{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"id":"load","values":[1,2]}` {
		t.Fatalf("单变量请求体 = %s", raw)
	}
}

// 多变量响应里 point/quantiles 是二维的，客户端要取第一个通道（= 目标序列）。
func TestClientForecastMultivariateResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		series := body["series"].([]any)[0].(map[string]any)
		values, ok := series["values"].([]any)
		if !ok || len(values) != 2 {
			t.Errorf("请求没按二维发出去: %+v", series["values"])
		}
		_, _ = w.Write([]byte(`{"success":true,"model":"timesfm-3.0","horizon":2,
			"forecasts":[{"id":"real","point":[[100,200],[900,950]],
			"quantiles":{"0.5":[[100,200],[900,950]]}}]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	resp, err := c.Forecast(context.Background(), &SForecastRequest{
		Series:  []*SSeries{{Id: "real", Matrix: [][]float64{{1, 2}, {3, 4}}}},
		Horizon: 2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	item := resp.Forecasts[0]
	if len(item.Point) != 2 || item.Point[0] != 100 || item.Point[1] != 200 {
		t.Fatalf("Point 应取第一个通道: %+v", item.Point)
	}
	if got := item.Quantiles["0.5"]; len(got) != 2 || got[0] != 100 || got[1] != 200 {
		t.Fatalf("Quantiles 应取第一个通道: %+v", item.Quantiles)
	}
}
