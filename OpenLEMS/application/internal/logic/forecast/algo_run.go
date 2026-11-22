package forecast

import (
	"context"
	"fmt"
	"strings"
	"time"

	"s_db/s_db_model"
)

// RunHourForecastRange 对 [from, to] 里的每个目标日、每个市场 as-of 跑一次小时预测，
// 并把结果写成留痕。历史日也能跑：锚点由 loader 按「目标日前最新完整 24 点日」自己推，
// 不依赖今天，所以补历史不用等每日任务慢慢积累。
//
// 与「看统计」解耦：这里只跑模型、写 gd_price_forecast；指标由 algo-review 按留痕算。
func RunHourForecastRange(ctx context.Context, from, to string, markets []string) (int, map[string]int, []string) {
	return runHourForecastRange(ctx, from, to, markets, loadHourCached)
}

func runHourForecastRange(
	ctx context.Context,
	from, to string,
	markets []string,
	load dailyRecordHourLoader,
) (days int, written map[string]int, skipped []string) {
	loc := priceLocation()
	start, err := time.ParseInLocation(priceDateLayout, from, loc)
	if err != nil {
		return 0, map[string]int{}, []string{"from 必须是 YYYY-MM-DD"}
	}
	end, err := time.ParseInLocation(priceDateLayout, to, loc)
	if err != nil {
		return 0, map[string]int{}, []string{"to 必须是 YYYY-MM-DD"}
	}
	if end.Before(start) {
		return 0, map[string]int{}, []string{"to 不能早于 from"}
	}

	written = make(map[string]int, len(markets))
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		days++
		date := d.Format(priceDateLayout)
		for _, market := range markets {
			// Compare=true：同一锚点把 GBM 也跑一遍，两个算法才可比；
			// ForceRefresh=true：回放不读页面缓存，按目标日能拿到的最新锚点算。
			res, lerr := load(ctx, PriceRequest{
				Market:       market,
				Source:       s_db_model.PriceSourceHome24h,
				Grain:        s_db_model.ForecastGrainHour,
				TargetDate:   date,
				ForceRefresh: true,
				Compare:      true,
			})
			if lerr != nil {
				skipped = append(skipped, fmt.Sprintf("%s/%s：%v", date, market, lerr))
				continue
			}
			if res == nil || strings.TrimSpace(res.To) == "" {
				skipped = append(skipped, fmt.Sprintf("%s/%s：结果缺少锚点", date, market))
				continue
			}

			n := recordHourPoints(ctx, market, res.To, res.Model, res.Unit, res.Points, s_db_model.ForecastAlgoTimesFM)
			if res.GBM != nil {
				n += recordHourPoints(ctx, market, res.To, res.GBM.Model, res.Unit, res.GBM.Points, s_db_model.ForecastAlgoGBM)
			} else if res.GBMError != "" {
				skipped = append(skipped, fmt.Sprintf("%s/%s：GBM 不可用（%s）", date, market, res.GBMError))
			}
			if n == 0 {
				skipped = append(skipped, fmt.Sprintf("%s/%s：没有可写入的 24 点结果", date, market))
				continue
			}
			written[market] += n
		}
	}
	return days, written, skipped
}
