package internal

// SSeries 一组输入序列。Values 为一维为单变量；二维为多变量（本服务暂用一维）。
type SSeries struct {
	Id     string    `json:"id,omitempty"`
	Values []float64 `json:"values"`
}

// SForecastRequest 预测请求，对应 TimesFM /v1/forecast。
type SForecastRequest struct {
	Series    []*SSeries `json:"series"`
	Horizon   int        `json:"horizon"`
	Quantiles []float64  `json:"quantiles,omitempty"`
}

// SForecastItem 单条预测结果。Point 为点预测，Quantiles 键为分位字符串（如 "0.5"）。
type SForecastItem struct {
	Id        string               `json:"id,omitempty"`
	Point     []float64            `json:"point"`
	Quantiles map[string][]float64 `json:"quantiles,omitempty"`
}

// SForecastResponse 预测响应。
type SForecastResponse struct {
	Success        bool             `json:"success"`
	Model          string           `json:"model"`
	Horizon        int              `json:"horizon"`
	Quantiles      []float64        `json:"quantiles"`
	Forecasts      []*SForecastItem `json:"forecasts"`
	ContextLengths []int            `json:"context_lengths"`
	TimeMs         float64          `json:"time_ms"`
	Truncated      bool             `json:"truncated"`

	// 失败时的错误信息
	Error  string `json:"error,omitempty"`
	Detail string `json:"detail,omitempty"`
}
