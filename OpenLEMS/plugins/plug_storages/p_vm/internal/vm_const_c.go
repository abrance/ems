package internal

// VictoriaMetrics 相关常量。
//
// 集群版把读写拆在两个组件上：vminsert 收样本，vmselect 出 PromQL。
// 路径里的 /insert/0 / /select/0 是租户（accountID=0）。
const (
	// DefaultInsertPath 单机版/集群版通用的 prometheus 文本导入路径
	DefaultInsertPath = "/insert/0/prometheus/api/v1/import/prometheus"
	// DefaultSelectPath 集群版 vmselect 的查询前缀（其后接 /api/v1/query_range）
	DefaultSelectPath = "/select/0/prometheus"

	// ParamInsertURL 覆盖写入地址（写读不在同一端口，通常必须给）
	ParamInsertURL = "insert_url"
	// ParamSelectURL 覆盖查询地址
	ParamSelectURL = "select_url"
	// ParamTimeoutMS 单次 HTTP 超时（毫秒）
	ParamTimeoutMS = "timeout_ms"

	// MetricNameEmsMetric 与 p_tsdb 相同的通用指标名，保证 ems 已有图表不用改
	MetricNameEmsMetric = "ems_metric"

	LabelNameMetric   = "__name__"
	LabelNameType     = "type"
	LabelNameID       = "id"
	LabelNameField    = "field"
	LabelNameDeviceID = "device_id"

	// 批量写入的单个请求最多带多少行样本
	writeBatchLines = 5000
	// 单次查询最多几条序列（pointKey 个数上限）
	maxQueryKeys = 50
)
