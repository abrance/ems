package forecast

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"common/c_log"
	"s_db/s_db_model"

	"github.com/gogf/gf/v2/frame/g"
)

// 每日日均价预测留痕。
//
// 复盘页的「当时预测」腿只能靠留痕长出来：RecordPriceForecast 原来只在有人请求
// /api/forecast/price 时才会被调用，没人看页面就没有当天记录。这里在进程内每天
// 主动算一次 day 粒度并写库。
//
// 三条硬口径（错一条数据就白跑）：
//  1. 只做 grain=day。官方没有合规的 96 点分时真值，做了也配不上对。
//  2. target_time = 生成日次日（Asia/Shanghai）。generated_on < target_time，
//     才不会被 forecast_v1_review.go 的「生成日晚于目标日=事后回填」过滤剔掉。
//  3. 不复用页面缓存（ForceRefresh=true），按执行时刻能拿到的最新官方日频数据重算；
//     写入走 RecordPriceForecast，唯一键 (generated_on, market, grain, target_time)
//     保证同一天重复执行只覆盖、不堆行。
//
// 真值/输入缺失、目标日不在预测范围内、预测值为 0 或非有限数时一律跳过并记日志，
// 不写假数、不写 0。

const (
	// dailyRecordStartupDelay 启动后延迟：等数据库与设置就绪，避免和首次数据加载抢资源。
	dailyRecordStartupDelay = 90 * time.Second
	// dailyRecordInterval 每天一次。进程长期运行时按启动时刻每 24h 跑一次；
	// 重启会补跑一次，唯一键保证不会堆重复行。
	dailyRecordInterval = 24 * time.Hour
	// dailyRecordSource 留痕来源说明，与页面手动预测保持同一口径。
	dailyRecordSource = "官方直采日均成交价"
)

// dailyRecordMarkets 与预测页/预测接口支持的市场一致（resolvePriceSpec 只认这两个）。
var dailyRecordMarkets = []string{"ahead", "real"}

// dailyRecordLoader 便于单测注入预测结果；生产使用 loadDaily（与接口同一条日频计算路径）。
type dailyRecordLoader func(context.Context, PriceRequest) (*TPriceForecastResult, error)

// Start 启动每日留痕（幂等：重复调用只起一个协程，由 ctx 控制生命周期）。
// 关掉的办法：配置 forecast.daily_record.enabled=false。默认 true ——
// 关掉只会让复盘「当时预测」腿停止长新数据，不动已有留痕，也不影响页面手动预测。
func Start(ctx context.Context) {
	enabled := g.Cfg().MustGet(ctx, "forecast.daily_record.enabled", true).Bool()
	if !enabled {
		c_log.Infof(ctx, "每日电价预测留痕已关闭（forecast.daily_record.enabled=false）")
		return
	}

	go func() {
		timer := time.NewTimer(dailyRecordStartupDelay)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				RunDailyRecordOnce(ctx, time.Now())
				timer.Reset(dailyRecordInterval)
			}
		}
	}()
	c_log.Infof(ctx, "每日电价预测留痕已启动：每 %s 一次（首次 %s 后）", dailyRecordInterval, dailyRecordStartupDelay)
}

// RunDailyRecordOnce 跑一轮：每个市场各写一条「生成日次日」的 day 粒度留痕。
// 返回每个市场实际写入的条数；失败的市场不在返回值里（原因已记日志）。
func RunDailyRecordOnce(ctx context.Context, now time.Time) map[string]int {
	return runDailyRecord(ctx, now, loadDaily)
}

func runDailyRecord(ctx context.Context, now time.Time, load dailyRecordLoader) map[string]int {
	written := make(map[string]int, len(dailyRecordMarkets))

	loc := priceLocation()
	triggerDate := now.In(loc).Format(priceDateLayout)

	for _, market := range dailyRecordMarkets {
		// ForceRefresh=true：不读页面那层 5 分钟缓存，按这次执行时刻能拿到的最新数据重算。
		res, err := load(ctx, PriceRequest{
			Market:       market,
			Grain:        s_db_model.ForecastGrainDay,
			ForceRefresh: true,
		})
		if err != nil {
			c_log.Warningf(ctx, "每日留痕跳过 %s：预测失败，不写假数（%+v）", market, err)
			continue
		}

		// 生成日取「真正算出结果」的时刻，不是触发时刻：23:59:5x 触发、模型算完已过零点时，
		// 用触发日会把留痕标成早一天生成，等于凭空多出一天的提前量。
		// loadDaily 在拿到模型结果后才打这个戳，是权威值；为空才回落到触发日。
		generatedOn := strings.TrimSpace(res.GeneratedOn)
		if generatedOn == "" {
			generatedOn = triggerDate
		}
		targetDate := nextDate(generatedOn)
		if targetDate == "" {
			c_log.Warningf(ctx, "每日留痕跳过 %s：生成日 %q 不是合法日期", market, generatedOn)
			continue
		}

		row, err := BuildDailyRecordRow(res, market, generatedOn, targetDate)
		if err != nil {
			c_log.Warningf(ctx, "每日留痕跳过 %s：%+v", market, err)
			continue
		}

		n, err := RecordPriceForecast(ctx, []*s_db_model.SPriceForecastModel{row})
		if err != nil {
			c_log.Warningf(ctx, "每日留痕写入失败 %s：%+v", market, err)
			continue
		}
		written[market] = n
		c_log.BizInfof(ctx, "每日电价留痕已写入：%s 生成 %s / 目标 %s（%.2f %s）",
			market, row.GeneratedOn, row.TargetTime, row.Point, row.Unit)
	}

	return written
}

// nextDate 返回 date 的次日；date 不是 YYYY-MM-DD 时返回空串，调用方据此跳过不写假数。
func nextDate(date string) string {
	d := parseDayTarget(date)
	if d.IsZero() {
		return ""
	}
	return d.AddDate(0, 0, 1).Format(priceDateLayout)
}

// BuildDailyRecordRow 从一次日频预测结果里取出目标日那一点，拼成一条留痕。
//
// 只取 targetDate 这一天：接口一次会返回未来多日，若把整段都写进去，同一天就会有
// 多条目标日不同的记录，既有生成日之前的（会被复盘当回填剔除），也有更远的日子，
// 不符合「每天每个市场一条当日留痕」的口径。
func BuildDailyRecordRow(
	res *TPriceForecastResult, market, generatedOn, targetDate string,
) (*s_db_model.SPriceForecastModel, error) {
	if res == nil {
		return nil, fmt.Errorf("预测结果为空")
	}
	if res.Grain != s_db_model.ForecastGrainDay {
		return nil, fmt.Errorf("预测粒度是 %q，每日留痕只允许 day", res.Grain)
	}
	if strings.TrimSpace(generatedOn) >= strings.TrimSpace(targetDate) {
		return nil, fmt.Errorf("生成日 %s 不早于目标日 %s，会被复盘当回填剔除", generatedOn, targetDate)
	}

	for _, p := range res.Points {
		if formatTargetTime(res.Grain, p.TargetTime) != targetDate {
			continue
		}
		if p.TargetTime.IsZero() || math.IsNaN(p.Point) || math.IsInf(p.Point, 0) {
			return nil, fmt.Errorf("目标日 %s 的预测值无效", targetDate)
		}
		if p.Point == 0 {
			return nil, fmt.Errorf("目标日 %s 的预测值为 0，疑似缺值", targetDate)
		}

		if m := strings.TrimSpace(res.Market); m != "" {
			market = m
		}
		return &s_db_model.SPriceForecastModel{
			GeneratedOn: strings.TrimSpace(generatedOn),
			Market:      market,
			Grain:       res.Grain,
			TargetTime:  targetDate,
			Point:       p.Point,
			Q10:         p.Q10,
			Q50:         p.Q50,
			Q90:         p.Q90,
			Model:       res.Model,
			Unit:        res.Unit,
			Source:      dailyRecordSource,
		}, nil
	}

	return nil, fmt.Errorf("预测结果里没有目标日 %s 的点（历史数据截止 %q）", targetDate, res.To)
}

// IsBackfilledForecast 复盘「当时预测」腿的回填过滤：
// 生成日晚于目标日 = 事后补算的历史值，不是当时发出去的预测。
// 抽成函数是为了让留痕任务与复盘页共用同一条判断，单测能直接钉住「不会被剔除」。
func IsBackfilledForecast(generatedOn, targetTime string) bool {
	return strings.TrimSpace(generatedOn) > forecastDatePart(targetTime)
}

func forecastDatePart(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > len(priceDateLayout) {
		return strings.TrimSpace(s[:len(priceDateLayout)])
	}
	return s
}
