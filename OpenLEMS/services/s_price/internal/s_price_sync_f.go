package internal

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"common/c_enum"
	"common/c_log"
	"s_db"
	"s_db/s_db_model"

	"github.com/gogf/gf/v2/container/gvar"
	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/gconv"
	"github.com/pkg/errors"
)

// priceSyncConfig 电网电价同步配置
type priceSyncConfig struct {
	Enabled        bool   // 是否启用同步
	BaseURL        string // 数据源地址
	Region         int    // 地区ID
	Usage          int    // 用电类型
	Cost           int    // 计费方式
	Voltage        int    // 电压等级
	IntervalHours  int    // 同步间隔（小时）
	LookbackMonths int    // 当前月无数据时向前回溯的月数
	Priority       int    // 写入电价的优先级（数值越小优先级越高）
	Description    string // 电价描述前缀
}

// StartEnergydcPriceSync 启动电网电价同步，按配置间隔在后台拉取并写入电价表
func StartEnergydcPriceSync(ctx context.Context) error {
	cfg := loadPriceSyncConfig(ctx)
	if !cfg.Enabled {
		c_log.Info(ctx, "电网电价同步未启用，跳过")
		return nil
	}

	interval := time.Duration(cfg.IntervalHours) * time.Hour
	if interval <= 0 {
		interval = 12 * time.Hour
	}

	go func() {
		c_log.BizInfof(ctx, "电网电价同步已启动 - 地区: %d, 间隔: %s", cfg.Region, interval)

		if err := syncEnergydcPrices(ctx, cfg); err != nil {
			c_log.Errorf(ctx, "电网电价同步失败: %+v", err)
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				c_log.Info(ctx, "电网电价同步已停止")
				return
			case <-ticker.C:
				if err := syncEnergydcPrices(ctx, cfg); err != nil {
					c_log.Errorf(ctx, "电网电价同步失败: %+v", err)
				}
			}
		}
	}()

	return nil
}

// loadPriceSyncConfig 读取同步配置，未配置时使用默认值
func loadPriceSyncConfig(ctx context.Context) priceSyncConfig {
	cfg := priceSyncConfig{
		Enabled:        false,
		BaseURL:        "https://energydc.cn/edc/api/v1",
		Region:         40,
		Usage:          24,
		Cost:           12,
		Voltage:        19,
		IntervalHours:  12,
		LookbackMonths: 6,
		Priority:       1,
		Description:    "电网分时电价",
	}

	cfg.Enabled = g.Cfg().MustGet(ctx, "price_sync.enabled", cfg.Enabled).Bool()
	cfg.BaseURL = g.Cfg().MustGet(ctx, "price_sync.base_url", cfg.BaseURL).String()
	cfg.Region = g.Cfg().MustGet(ctx, "price_sync.region", cfg.Region).Int()
	cfg.Usage = g.Cfg().MustGet(ctx, "price_sync.usage", cfg.Usage).Int()
	cfg.Cost = g.Cfg().MustGet(ctx, "price_sync.cost", cfg.Cost).Int()
	cfg.Voltage = g.Cfg().MustGet(ctx, "price_sync.voltage", cfg.Voltage).Int()
	cfg.IntervalHours = g.Cfg().MustGet(ctx, "price_sync.interval_hours", cfg.IntervalHours).Int()
	cfg.LookbackMonths = g.Cfg().MustGet(ctx, "price_sync.lookback_months", cfg.LookbackMonths).Int()
	cfg.Priority = g.Cfg().MustGet(ctx, "price_sync.priority", cfg.Priority).Int()
	cfg.Description = g.Cfg().MustGet(ctx, "price_sync.description", cfg.Description).String()

	if cfg.Priority < 1 || cfg.Priority > 5 {
		cfg.Priority = 1
	}
	if cfg.LookbackMonths < 0 {
		cfg.LookbackMonths = 0
	}
	return cfg
}

// syncEnergydcPrices 拉取最新可用月份的电价并写入电价表
func syncEnergydcPrices(ctx context.Context, cfg priceSyncConfig) error {
	record, err := fetchLatestPrice(ctx, cfg)
	if err != nil {
		return err
	}
	if record == nil {
		return errors.New("数据源未返回任何电价记录")
	}

	model, err := buildPriceModel(cfg, record)
	if err != nil {
		return errors.Wrap(err, "转换电价记录失败")
	}
	if err := upsertPrice(ctx, model); err != nil {
		return errors.Wrap(err, "写入电价失败")
	}

	if err := GetPriceCache().RefreshCache(ctx); err != nil {
		return errors.Wrap(err, "刷新电价缓存失败")
	}
	c_log.Infof(ctx, "电网电价已同步 - remoteId: %s, 描述: %s", model.RemoteId, model.Description)
	return nil
}

// fetchLatestPrice 从当前月开始向前回溯，返回首个有数据的月份记录
func fetchLatestPrice(ctx context.Context, cfg priceSyncConfig) (*gjson.Json, error) {
	now := time.Now()
	for offset := 0; offset <= cfg.LookbackMonths; offset++ {
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).
			AddDate(0, -offset, 0)

		list, err := requestPriceList(ctx, cfg, monthStart.Unix())
		if err != nil {
			return nil, err
		}
		if len(list) > 0 {
			return list[0], nil
		}
		c_log.Debugf(ctx, "电网电价同步：%s 无数据，继续回溯", monthStart.Format("2006-01"))
	}
	return nil, nil
}

// requestPriceList 请求指定时间点所属的电价记录。
// 注意：该接口只需 startTime，携带 endTime 时不会返回结果。
func requestPriceList(ctx context.Context, cfg priceSyncConfig, startTime int64) ([]*gjson.Json, error) {
	reqBody := g.Map{
		"startTime": startTime,
		"region":    cfg.Region,
		"usage":     cfg.Usage,
		"cost":      cfg.Cost,
		"voltage":   cfg.Voltage,
	}

	client := g.Client().Timeout(15 * time.Second)
	client.SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	client.SetHeader("Origin", "https://energydc.cn")
	client.SetHeader("Referer", "https://energydc.cn/")

	resp, err := client.ContentJson().Post(ctx, strings.TrimRight(cfg.BaseURL, "/")+"/eprice/price-list", reqBody)
	if err != nil {
		return nil, errors.Wrap(err, "请求电网电价接口失败")
	}
	defer resp.Close()

	body := gjson.New(resp.ReadAllString())
	if code := body.Get("code").Int(); code != 200 {
		return nil, errors.Errorf("电网电价接口返回异常 - code: %d, message: %s", code, body.Get("message").String())
	}

	items := body.Get("data.list").Array()
	list := make([]*gjson.Json, 0, len(items))
	for _, item := range items {
		list = append(list, gjson.New(item))
	}
	return list, nil
}

// buildPriceModel 将一条远程电价记录转换为电价表模型
func buildPriceModel(cfg priceSyncConfig, record *gjson.Json) (*s_db_model.SPriceModel, error) {
	stage := parseStage(record)

	segments := buildSegments(record, stage)
	if len(segments) == 0 {
		return nil, errors.New("时段配置为空")
	}

	start := normalizeUnix(record.Get("startTime").Int64())
	end := normalizeUnix(record.Get("endTime").Int64())

	dateRange := SDateRange{
		StartDate: start.Format("2006-01-02"),
		EndDate:   end.Format("2006-01-02"),
		// 电网按月发布，最新可得月份可能滞后于当前日期，按长期有效处理
		IsLongTerm: true,
	}
	timeRange := STimeRange{
		Type:        "weekday",
		WeekdayType: "all",
	}

	remoteId := buildRemoteId(record)
	description := fmt.Sprintf("%s %s", cfg.Description, start.Format("2006-01"))

	return &s_db_model.SPriceModel{
		Description:   description,
		Priority:      cfg.Priority,
		Status:        "Enable",
		DateRange:     gjson.MustEncodeString(dateRange),
		TimeRange:     gjson.MustEncodeString(timeRange),
		PriceSegments: gjson.MustEncodeString(segments),
		RemoteId:      remoteId,
		CreatedBy:     "energydc",
	}, nil
}

// buildSegments 根据远程记录构建按开始时间排序的电价时段
func buildSegments(record, stage *gjson.Json) []*SPriceSegment {
	var segments []*SPriceSegment
	appendSegments := func(priceKey string, stageKey string, priceType c_enum.EPriceType) {
		price := record.Get(priceKey).Float64()
		if price <= 0 {
			return
		}
		for _, r := range parseTimeRanges(stage.Get(stageKey)) {
			start, end, ok := splitTimeRange(r)
			if !ok {
				continue
			}
			segments = append(segments, &SPriceSegment{
				StartTime: start,
				EndTime:   end,
				PriceType: priceType,
				Price:     price,
			})
		}
	}

	appendSegments("top", "top", c_enum.EPriceTypeSharp)
	appendSegments("peak", "peak", c_enum.EPriceTypePeak)
	appendSegments("normal", "normal", c_enum.EPriceTypeFlat)
	appendSegments("valley", "valley", c_enum.EPriceTypeValley)
	appendSegments("deep", "deep", c_enum.EPriceTypeDeepValley)

	sort.Slice(segments, func(i, j int) bool {
		return timeToMinutes(segments[i].StartTime) < timeToMinutes(segments[j].StartTime)
	})
	return segments
}

// parseTimeRanges 解析时段字段。该字段可能是数组，也可能是数组的 JSON 字符串
func parseTimeRanges(value *gvar.Var) []string {
	if value == nil || value.IsNil() {
		return nil
	}
	text := strings.TrimSpace(value.String())
	if text == "" || text == "[]" {
		return nil
	}
	if !strings.HasPrefix(text, "[") {
		return nil
	}
	items := gjson.New(text).Array()
	ranges := make([]string, 0, len(items))
	for _, item := range items {
		ranges = append(ranges, gconv.String(item))
	}
	return ranges
}

// parseStage 解析 stage 字段，兼容对象与其 JSON 字符串两种形式
func parseStage(record *gjson.Json) *gjson.Json {
	raw := record.Get("stage")
	if raw == nil || raw.IsNil() {
		return gjson.New("{}")
	}
	if raw.String() == "" {
		return gjson.New("{}")
	}
	// 数组型字段时 Var().String() 会返回 JSON 文本，这里统一重新解析
	return gjson.New(raw.String())
}

// buildRemoteId 构造稳定且唯一的远程标识，优先使用电网政策UUID
func buildRemoteId(record *gjson.Json) string {
	if uuid := strings.TrimSpace(record.Get("policyUuid").String()); uuid != "" {
		return "energydc:" + uuid
	}
	return fmt.Sprintf("energydc:%d:%d:%d:%d:%d",
		record.Get("regionId").Int(),
		record.Get("usage").Int(),
		record.Get("cost").Int(),
		record.Get("voltage").Int(),
		record.Get("startTime").Int64(),
	)
}

// normalizeUnix 兼容秒与毫秒时间戳
func normalizeUnix(value int64) time.Time {
	if value > 1e12 {
		return time.UnixMilli(value).In(priceSyncLocation)
	}
	return time.Unix(value, 0).In(priceSyncLocation)
}

// priceSyncLocation 电网数据按东八区发布
var priceSyncLocation = time.FixedZone("CST", 8*3600)

// splitTimeRange 解析 "HH:MM-HH:MM" 时段
func splitTimeRange(value string) (string, string, bool) {
	parts := strings.SplitN(strings.TrimSpace(value), "-", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	start := normalizeHHMM(parts[0])
	end := normalizeHHMM(parts[1])
	if start == "" || end == "" {
		return "", "", false
	}
	return start, end, true
}

// normalizeHHMM 规范化时间字符串，24:00 归一为 23:59
func normalizeHHMM(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if value == "24:00" {
		return "23:59"
	}
	if _, err := time.Parse("15:04", value); err != nil {
		return ""
	}
	return value
}

// timeToMinutes 将 "HH:MM" 转为分钟数
func timeToMinutes(value string) int {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0
	}
	return parsed.Hour()*60 + parsed.Minute()
}

// upsertPrice 按 remoteId 新增或更新电价，并停用其他已同步的电网电价
func upsertPrice(ctx context.Context, model *s_db_model.SPriceModel) error {
	service := s_db.GetPriceService()

	all, err := service.GetAllPrices(ctx)
	if err != nil {
		return errors.Wrap(err, "查询电价列表失败")
	}
	for _, price := range all {
		if price.Id == model.Id || price.Status != "Enable" {
			continue
		}
		if strings.HasPrefix(price.RemoteId, "energydc:") && price.RemoteId != model.RemoteId {
			price.Status = "Disable"
			if err := service.UpdatePrice(ctx, price); err != nil {
				c_log.Errorf(ctx, "停用历史电网电价失败 - id: %d, 错误: %+v", price.Id, err)
			}
		}
	}

	existing, err := service.GetPricesByRemoteId(ctx, model.RemoteId)
	if err != nil {
		return errors.Wrap(err, "查询已存在电价失败")
	}

	if len(existing) > 0 {
		model.Id = existing[0].Id
		return service.UpdatePrice(ctx, model)
	}

	_, err = service.CreatePrice(ctx, model)
	return err
}
