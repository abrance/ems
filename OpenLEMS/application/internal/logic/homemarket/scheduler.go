package homemarket

import (
	"context"
	"time"

	"common/c_log"
	"github.com/gogf/gf/v2/frame/g"
)

// 采集调度。
//
// 与 gd_spot_metric 的 pm-api 采集**分开**：那套是 tools/gdpm 的 Python 脚本 + 外部 cron，
// 写 gd_spot_metric 与 gd_pm_fetch_run；这里跑在 ems 进程内，写 gd_home_24h / gd_node_price。
// 两者不共享定时器、不共享失败语义 —— 一边挂了另一边照常。
//
// 间隔定成 30 分钟：24 点 T-6 才出、地市 T+0，一天真正变化的只有一次，
// 30 分钟足够当天内补上，也不会把源站打得太勤（源站有动态防护）。

const (
	// DefaultInterval 采集间隔
	DefaultInterval = 30 * time.Minute
	// startupDelay 启动后延迟：让数据库与设置先就绪，避免开机瞬间抢资源
	startupDelay = 90 * time.Second
)

// MaxBackfillDays 手动回补的窗口上限。再大就不是「补洞」而是「导数据」了，
// 而源站有动态防护：一次 30 天 = 30 次 24 点 + 30 次地市，已经够呛。
const MaxBackfillDays = 30

// Start 启动采集（幂等：重复调用只起一个协程由 ctx 控制生命周期）。
//
// 关掉的办法：配置 home_market.enabled=false。默认 true —— 这两路是页面的数据源，
// 关掉只会让页面报「该日无数据」，不会静默用旧数顶上。
func Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultInterval
	}
	enabled := g.Cfg().MustGet(ctx, "home_market.enabled", true).Bool()
	if !enabled {
		c_log.Infof(ctx, "官网主页市场数据采集已关闭（home_market.enabled=false）")
		return
	}
	backfillDays := ParseDays(g.Cfg().MustGet(ctx, "home_market.backfill_days", defaultBackfillDays).String())

	go func() {
		timer := time.NewTimer(startupDelay)
		defer timer.Stop()
		first := true
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				runOnce(ctx, backfillDays, first)
				first = false
				timer.Reset(interval)
			}
		}
	}()
	c_log.Infof(ctx, "官网主页市场数据采集已启动：每 %s 一次（首次 %s 后）", interval, startupDelay)
}

// RunBackfill 手动回补一次两路（供运维接口调用）。
//
// days 是**地市**的窗口（T+0 口径，含当天）；24 点的窗口由 Home24hWindowDays
// 单独换算（源站 T-6 才发布，共用一个数字会让整窗只剩一天有数）。
// 失败只记日志、不补假数据，与自动采集同一套语义 —— 调用方看返回值就知道哪几天真写进去了。
func RunBackfill(ctx context.Context, now time.Time, days int) (map[string]int, map[string]int) {
	nodeDays := clampDays(days)
	home24hWindow := Home24hWindowDays(nodeDays)
	from := now.In(time.Local).AddDate(0, 0, -(home24hWindow - 1))
	home24h, err := BackfillHome24h(ctx, from, now)
	if err != nil {
		c_log.Warningf(ctx, "手动回补 24 点分时中断（不补假数据）：%+v，已写入 %v", err, home24h)
	}
	node := BackfillNodePrice(ctx, now, nodeDays)
	return home24h, node
}

// clampDays 把回补窗口夹到 [默认值, 上限]。
func clampDays(days int) int {
	if days <= 0 {
		days = defaultBackfillDays
	}
	if days > MaxBackfillDays {
		days = MaxBackfillDays
	}
	return days
}

// runOnce 一次采集：24 点取最近有数的一天，地市按最近 N 天回补。
//
// first 为 true 时两路都做一次回补（24 点按 Home24hWindowDays，地市按 backfillDays）；
// 之后每次只采当天 —— 回补是「补历史缺口」，每次定时都重扫窗口既浪费请求也容易触发源站限流。
//
// 24 点必须回补而不能只靠 CollectHome24h：它探到「最近一个有数的一天」就停手，
// 进程停机跨过一个发布日时，恢复后只会采到最新那天，中间几天永久缺。
func runOnce(ctx context.Context, backfillDays int, first bool) {
	now := time.Now()

	if first {
		// 首次启动：两路一起按窗口回补（与手动接口同一份实现，口径不会分叉）。
		home24h, node := RunBackfill(ctx, now, backfillDays)
		c_log.BizInfof(ctx, "首次启动回补完成：24 点 %v；地市 %v", home24h, node)
		return
	}

	date, n, err := CollectHome24h(ctx, now)
	if err != nil {
		// 按类别打：blocked（防护/网关）与 network/parse 的处置不同，不能混成一句
		c_log.Warningf(ctx, "采集 24 点分时失败[%s]：%+v", classifyErr(err), err)
	} else if n > 0 {
		c_log.BizInfof(ctx, "24 点分时已更新：%s（%d 点）", date, n)
	} else {
		// 只有「每轮请求都成功且 payload 明确为空」才走到这里（CollectHome24h 已把失败抛成 error）
		c_log.Infof(ctx, "24 点分时最近 %d 天都没有数据（源站未发布）", maxLookbackDays)
	}

	today := now.Format(dateLayout)
	if m, err := CollectNodePrice(ctx, today); err != nil {
		c_log.Warningf(ctx, "采集地市节点价 %s 失败[%s]：%+v", today, classifyErr(err), err)
	} else if m > 0 {
		c_log.BizInfof(ctx, "地市节点价已更新：%s（%d 条）", today, m)
	}
}
