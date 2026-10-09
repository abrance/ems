package internal

import (
	"encoding/json"

	"github.com/pkg/errors"
)

// SSeries 一组输入序列。
//
// Values 是一维（单变量）；Matrix 是二维（多变量，外层 = 通道，内层 = 该通道的序列值）。
// 只能用一个：Matrix 非空时按二维发送。多变量时**第一个通道是目标序列** ——
// 响应里的 Point/Quantiles 取的就是它（见 SForecastItem.UnmarshalJSON）。
type SSeries struct {
	Id     string    `json:"id,omitempty"`
	Values []float64 `json:"values,omitempty"`
	// Matrix 多变量输入。为什么需要：实时电价预测把「日前电价 + 系统负荷」当额外通道送进去，
	// 实测（2026-09-08 ~ 10-01，24 天真实实际值）MAE 96.4 → 89.5 元/MWh。
	// 日前预测同样测过（加负荷 / 加实时价 / 加星期几），全部持平或更差，保持单变量。
	Matrix [][]float64 `json:"-"`
}

// MarshalJSON 按「一维或二维」发送 values：服务端两种形状都收。
func (s SSeries) MarshalJSON() ([]byte, error) {
	values, err := json.Marshal(s.Values)
	if err != nil {
		return nil, err
	}
	if len(s.Matrix) > 0 {
		if values, err = json.Marshal(s.Matrix); err != nil {
			return nil, err
		}
	}
	type wire struct {
		Id     string          `json:"id,omitempty"`
		Values json.RawMessage `json:"values"`
	}
	return json.Marshal(wire{Id: s.Id, Values: values})
}

// SForecastRequest 预测请求，对应 TimesFM /v1/forecast。
type SForecastRequest struct {
	Series    []*SSeries `json:"series"`
	Horizon   int        `json:"horizon"`
	Quantiles []float64  `json:"quantiles,omitempty"`
}

// SForecastItem 单条预测结果。
//
// 服务在单变量输入时返回一维 point / quantiles（[]float64），多变量时返回二维
// （外层 = 通道）。这里统一取**第一个通道**（= 目标序列）：调用方读 Point/Quantiles
// 就行，不必关心自己送的是几维。
type SForecastItem struct {
	Id        string               `json:"id,omitempty"`
	Point     []float64            `json:"point"`
	Quantiles map[string][]float64 `json:"quantiles,omitempty"`
}

func (i *SForecastItem) UnmarshalJSON(data []byte) error {
	var raw struct {
		Id        string                     `json:"id"`
		Point     json.RawMessage            `json:"point"`
		Quantiles map[string]json.RawMessage `json:"quantiles"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	i.Id = raw.Id
	point, err := firstChannel(raw.Point)
	if err != nil {
		return errors.Wrap(err, "解析 point 失败")
	}
	i.Point = point
	if len(raw.Quantiles) > 0 {
		i.Quantiles = make(map[string][]float64, len(raw.Quantiles))
		for key, rawQ := range raw.Quantiles {
			arr, err := firstChannel(rawQ)
			if err != nil {
				return errors.Wrapf(err, "解析分位 %s 失败", key)
			}
			i.Quantiles[key] = arr
		}
	}
	return nil
}

// firstChannel 把「一维或二维」的序列解成第一个通道。
func firstChannel(raw json.RawMessage) ([]float64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var single []float64
	if err := json.Unmarshal(raw, &single); err == nil {
		return single, nil
	}
	var matrix [][]float64
	if err := json.Unmarshal(raw, &matrix); err != nil {
		return nil, err
	}
	if len(matrix) == 0 {
		return nil, nil
	}
	return matrix[0], nil
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
