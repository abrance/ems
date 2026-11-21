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
	// dailyRecordDailyOffset 每天的执行偏移：Asia/Shanghai 自然日 00:05 跑一次。
	//
	// 原来是固定 24h（按进程启动时刻起算），实际节奏等于重启频率而不是自然日：
	// 线上 11-09、11-10 一天留痕都没有，11-13 一天写了两次，复盘页
	// 「预测留痕自 X 起积累」的承诺因此不成立。改成自然日对齐后，只要进程活着，
	// 每天固定一条；重启仍由 90s 补跑兜住，唯一键保证不堆重复行。
	dailyRecordDailyOffset = 5 * time.Minute
	// dailyRecordSource 留痕来源说明，与页面手动预测保持同一口径。
	dailyRecordSource = "官方直采日均成交价"
)

// dailyRecordNow 取「现在」，生产用 time.Now，单测替换成固定时刻。
var dailyRecordNow = time.Now

// dailyRecordFirstWait 启动后的首次等待：90s 补跑，重启也能补上当天那条。
func dailyRecordFirstWait() time.Duration { return dailyRecordStartupDelay }

// nextDailyRun 返回 now 之后的下一次执行时刻：Asia/Shanghai 自然日 00:05。
//
// 严格 After：now 恰好是 00:05:00 时返回次日 00:05，不会原地重跑。
// 跨月/跨年/闰日靠 time.Date 的归一化（day+1 由它自己进位），不手算月份天数。
func nextDailyRun(now time.Time) time.Time {
	loc := priceLocation()
	local := now.In(loc)
	next := time.Date(local.Year(), local.Month(), local.Day(),
		0, int(dailyRecordDailyOffset/time.Minute), 0, 0, loc)
	if !next.After(local) {
		next = time.Date(local.Year(), local.Month(), local.Day()+1,
			0, int(dailyRecordDailyOffset/time.Minute), 0, 0, loc)
	}
	return next
}

// dailyRecordDelay 距下一次自然日执行还有多久。
func dailyRecordDelay(now time.Time) time.Duration {
	return nextDailyRun(now).Sub(now)
}

// dailyRecordMarkets 与预测页/预测接口支持的市场一致（resolvePriceSpec 只认这两个）。
var dailyRecordMarkets = []string{"ahead", "real"}

// dailyRecordLoader 便于单测注入预测结果；生产使用 loadDaily（与接口同一条日频计算路径）。
type dailyRecordLoader func(context.Context, PriceRequest) (*TPriceForecastResult, error)

// dailyRecordHourLoader 小时粒度入口；生产使用 loadHourCached（与接口同一条 24 点计算路径）。
// 与日频 loader 分开，是因为两者的请求参数（targetDate / compare）不同。
type dailyRecordHourLoader func(context.Context, PriceRequest) (*TPriceForecastResult, error)

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
		timer := time.NewTimer(dailyRecordFirstWait())
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				now := dailyRecordNow()
				RunDailyRecordOnce(ctx, now)
				// hour 留痕与日频同一时刻、同一轮触发：两种粒度各有自己的锚点，
				// 但「每天各算法至少一条」的保证一致。
				RunDailyHourRecordOnce(ctx, now)
				// 跑完再取时刻算间隔：一轮耗时长也不会算出负延迟导致立刻重跑。
				timer.Reset(dailyRecordDelay(dailyRecordNow()))
			}
		}
	}()
	c_log.Infof(ctx, "每日电价预测留痕已启动：每天 Asia/Shanghai %02d:%02d 跑一次（首次 %s 后补跑）",
		int(dailyRecordDailyOffset/time.Hour), int(dailyRecordDailyOffset/time.Minute)%60, dailyRecordStartupDelay)
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

// RunDailyHourRecordOnce 跑一轮小时留痕：每个市场在同一锚点上，把算法对比要用的
// 每个算法各写 24 行。页面请求不再写 hour 留痕（见 LoadPriceAny），这里是唯一来源。
// 返回每个市场实际写入的行数；失败的市场不在返回值里（原因已记日志）。
func RunDailyHourRecordOnce(ctx context.Context, now time.Time) map[string]int {
	return runDailyHourRecord(ctx, now, loadHourCached)
}

func runDailyHourRecord(ctx context.Context, now time.Time, load dailyRecordHourLoader) map[string]int {
	written := make(map[string]int, len(dailyRecordMarkets))
	targetDate := now.In(priceLocation()).Format(priceDateLayout)

	for _, market := range dailyRecordMarkets {
		// Compare=true：同一锚点下把 GBM 也跑一遍，两个算法才可比。
		// ForceRefresh=true：不读页面缓存，按执行时刻能拿到的最新数据定锚点。
		res, err := load(ctx, PriceRequest{
			Market:       market,
			Source:       s_db_model.PriceSourceHome24h,
			Grain:        s_db_model.ForecastGrainHour,
			TargetDate:   targetDate,
			ForceRefresh: true,
			Compare:      true,
		})
		if err != nil {
			c_log.Warningf(ctx, "每日小时留痕跳过 %s：预测失败，不写假数（%+v）", market, err)
			continue
		}
		if res == nil || strings.TrimSpace(res.To) == "" {
			c_log.Warningf(ctx, "每日小时留痕跳过 %s：结果缺少锚点", market)
			continue
		}

		n := recordHourPoints(ctx, market, res.To, res.Model, res.Unit, res.Points, s_db_model.ForecastAlgoTimesFM)
		if res.GBM != nil {
			n += recordHourPoints(ctx, market, res.To, res.GBM.Model, res.Unit, res.GBM.Points, s_db_model.ForecastAlgoGBM)
		} else if res.GBMError != "" {
			c_log.Warningf(ctx, "每日小时留痕 %s 的 GBM 不可用：%s", market, res.GBMError)
		}
		if n == 0 {
			continue
		}
		written[market] = n
		c_log.BizInfof(ctx, "每日小时留痕已写入：%s 锚点 %s / 目标 %s / %d 行", market, res.To, targetDate, n)
	}
	return written
}

// recordHourPoints 把一次 24 点预测写成留痕。algo 由调用方指定，唯一键含 algo，
// 两个算法写同一目标时刻不会互相覆盖。点数不是 24 就整段不写：半截样本进回测
// 会让「某算法缺一天」伪装成「那天误差很小」。
func recordHourPoints(
	ctx context.Context,
	market, generatedOn, model, unit string,
	points []TPriceForecastPoint,
	algo string,
) int {
	if len(points) != 24 {
		c_log.Warningf(ctx, "每日小时留痕 %s/%s 点数 = %d，需要 24，跳过", market, algo, len(points))
		return 0
	}
	rows := make([]*s_db_model.SPriceForecastModel, 0, 24)
	for _, p := range points {
		if p.TargetTime.IsZero() || math.IsNaN(p.Point) || math.IsInf(p.Point, 0) {
			continue
		}
		rows = append(rows, &s_db_model.SPriceForecastModel{
			GeneratedOn: strings.TrimSpace(generatedOn),
			Market:      market,
			Grain:       s_db_model.ForecastGrainHour,
			Algo:        algo,
			TargetTime:  formatTargetTime(s_db_model.ForecastGrainHour, p.TargetTime),
			Point:       p.Point,
			Q10:         p.Q10,
			Q50:         p.Q50,
			Q90:         p.Q90,
			Model:       model,
			Unit:        unit,
			Source:      leafSourceLabel(s_db_model.PriceSourceHome24h),
		})
	}
	if len(rows) != len(points) {
		c_log.Warningf(ctx, "每日小时留痕 %s/%s 有 %d 个无效点，跳过整段", market, algo, len(points)-len(rows))
		return 0
	}
	n, err := RecordPriceForecast(ctx, rows)
	if err != nil {
		c_log.Warningf(ctx, "每日小时留痕写入失败 %s/%s：%+v", market, algo, err)
		return 0
	}
	return n
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
			Algo:        s_db_model.ForecastAlgoTimesFM,
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
