package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientForecastOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/forecast" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		var req SForecastRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.Horizon != 4 || len(req.Series) != 1 || req.Series[0].Id != "load" {
			t.Errorf("unexpected request: %+v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"model":   "timesfm-3.0",
			"horizon": 4,
			"forecasts": []map[string]any{
				{"id": "load", "point": []float64{1, 2, 3, 4}, "quantiles": map[string][]float64{"0.5": {1, 2, 3, 4}}},
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	resp, err := c.Forecast(context.Background(), &SForecastRequest{
		Series:  []*SSeries{{Id: "load", Values: []float64{9, 8, 7}}},
		Horizon: 4,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Forecasts) != 1 || len(resp.Forecasts[0].Point) != 4 {
		t.Fatalf("unexpected response: %+v", resp.Forecasts)
	}
}

func TestClientForecastHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "horizon=1000 超过 MAX_HORIZON=512",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	_, err := c.Forecast(context.Background(), &SForecastRequest{Series: []*SSeries{{Values: []float64{1}}}, Horizon: 1000})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "MAX_HORIZON") {
		t.Errorf("error should contain server message, got: %v", err)
	}
}

func TestClientForecastSuccessFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "模型未加载"})
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, 5*time.Second).Forecast(context.Background(), &SForecastRequest{Series: []*SSeries{{Values: []float64{1}}}})
	if err == nil || !strings.Contains(err.Error(), "模型未加载") {
		t.Fatalf("expected success=false error, got: %v", err)
	}
}

func TestClientForecastInputGuards(t *testing.T) {
	if _, err := NewClient("http://example.test", time.Second).Forecast(context.Background(), &SForecastRequest{Horizon: 4}); err == nil {
		t.Error("空 series 应报错")
	}
	if _, err := NewClient("", time.Second).Forecast(context.Background(), &SForecastRequest{Series: []*SSeries{{Values: []float64{1}}}}); err == nil {
		t.Error("空 baseURL 应报错")
	}
}
