package internal

import (
	"strconv"
	"strings"
)

// sample 是一条待写入 VictoriaMetrics 的样本。
// TS 是毫秒时间戳（VM 的 prometheus 导入接口默认按毫秒解析）。
type sample struct {
	Name   string
	Labels map[string]string
	Value  float64
	TS     int64
}

// formatSamples 把样本渲染成 prometheus 文本行协议，一行一条样本。
//
// 只提供格式，不发请求，方便单测把「写进 VM 的 payload」钉死。
func formatSamples(samples []sample) string {
	var b strings.Builder
	for _, s := range samples {
		b.WriteString(s.Name)
		if len(s.Labels) > 0 {
			// 标签顺序固定，避免同一批数据每次生成不同文本（便于对比/测试）
			keys := make([]string, 0, len(s.Labels))
			for k := range s.Labels {
				keys = append(keys, k)
			}
			sortStrings(keys)
			b.WriteByte('{')
			for i, k := range keys {
				if i > 0 {
					b.WriteByte(',')
				}
				b.WriteString(k)
				b.WriteString(`="`)
				b.WriteString(escapeLabelValue(s.Labels[k]))
				b.WriteByte('"')
			}
			b.WriteByte('}')
		}
		b.WriteByte(' ')
		b.WriteString(strconv.FormatFloat(s.Value, 'f', -1, 64))
		b.WriteByte(' ')
		b.WriteString(strconv.FormatInt(s.TS, 10))
		b.WriteByte('\n')
	}
	return b.String()
}

// escapeLabelValue 转义标签值里的反斜杠、双引号和换行（prometheus 文本格式要求）。
func escapeLabelValue(v string) string {
	if !strings.ContainsAny(v, "\\\"\n") {
		return v
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return r.Replace(v)
}

func sortStrings(s []string) {
	// 小切片插入排序，免一个 sort 依赖也够用
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
