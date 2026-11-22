package cmd

import (
	"strings"
	"testing"

	"github.com/gogf/gf/v2/frame/g"
)

// 路由注册测试。
//
// 为什么需要：控制器方法只写在具体类型上，而 NewV1() 返回的是一个**只声明了部分方法**的接口。
// GoFrame 的 Bind 走反射拿到具体类型，所以只写具体方法也能注册 —— 但这件事没有任何东西守着。
// 一旦有人「顺手把接口补全」或改了返回类型，路由会静默少掉几个，而前端只会在运行时 404。
//
// 这里列出前端实际依赖的接口，少一个就失败。
func TestAPIRoutesRegistered(t *testing.T) {
	s := g.Server("test-routes")
	s.SetDumpRouterMap(false)
	s.SetAddr("127.0.0.1:0") // 随机端口，避免和本地服务撞
	setupAPIRoutes(s)
	// GF 的 Bind 是延迟到 Start 才落到路由表的，不启动就只能看到默认的 /* 一条
	s.Start()
	defer s.Shutdown()

	got := map[string]bool{}
	for _, r := range s.GetRoutes() {
		got[strings.ToUpper(r.Method)+" "+r.Route] = true
	}

	// 前端用到的接口（按模块分组，方便以后加一条就补一条）
	want := []string{
		// 认证（登录页与「用户与权限」依赖；改密码接口带 oldPassword 校验，见 controller/auth）
		"POST /api/auth/login",
		"POST /api/auth/logout",
		"POST /api/auth/change-password",
		"GET /api/auth/current",
		// 现货日报 / 电网日指标
		"GET /api/gd-metric",
		"GET /api/gd-metric/series",
		"GET /api/gd-metric/supply-curve",
		"GET /api/spot-report",
		"GET /api/spot-report/data",
		"GET /api/spot-report/trend",
		"GET /api/spot-report/audit",
		// 官方接口直采（pm-api）的电力市场指标面
		"GET /api/spot-market/board",
		// 官网主页两路市场数据（24 点分时 / 21 地市节点价），与上面两条都不是一路
		"GET /api/gd-metric/home-24h",
		"GET /api/gd-metric/node-price",
		// 两路采集状态（滞后/失败/停更），只讲能不能用、不给数值；不进 /api/health
		"GET /api/gd-metric/home-market/status",
		// 运维补洞：按窗口重跑两路回补（首次启动的自动回补补不了「某天采集失败」的洞）
		"POST /api/gd-metric/home-market/backfill",
		// 分时研究（第三方 · 非官方，只读冻结快照）
		"GET /api/research/intraday",
		// 日报核对（OCR 落独立表，右列读官方 pm-api）
		"GET /api/report-daily/reconcile",
		"POST /api/report-daily/extract",
		"POST /api/report-daily/extract/batch",
		"GET /api/report-daily/extract/batch/status",
		"POST /api/report-daily/backfill",
		"POST /api/spot-report/collect",
		"POST /api/spot-report/extract",
		"POST /api/spot-report/extract/batch",
		"GET /api/spot-report/extract/batch/status",
		"POST /api/spot-report/metric",
		"POST /api/spot-report/confirm",
		// 预测
		"GET /api/forecast/load",
		"GET /api/forecast/price",
		"GET /api/forecast/recorded",
		"GET /api/forecast/backtest",
		"GET /api/forecast/algo-review",
		"POST /api/forecast/algo-review/run",
		// 效果
		"GET /api/effect/summary",
		"GET /api/effect/backtest",
		// 策略 / 电价
		"GET /api/policy/config",
		"GET /api/policy/config-fields",
		"GET /api/strategy/energy-storage",
		"POST /api/strategy/energy-storage/activate",
		"GET /api/price",
		"GET /api/price/current",
		"PUT /api/price/{id}/toggle",
		// 设备 / 协议 / 远程 / 网络
		"GET /api/device/detailed-list",
		"GET /api/device/{deviceId}/points/definitions",
		"POST /api/device/enable",
		"GET /api/control/custom-services",
		"POST /api/control/device",
		"GET /api/protocol/list",
		"GET /api/driver/list",
		"GET /api/remote/modbus/status",
		"GET /api/network/interface/list",
		"POST /api/network/ping",
		// 自动化 / 系统
		"GET /api/automation/page",
		"POST /api/automation/{id}/toggle",
		"GET /api/system/summary",
		"GET /api/system/credentials",
		"GET /api/system/retention",
		"POST /api/system/retention/clean",
		"GET /api/system/storage/stats",
		"POST /api/system/reboot/apply",
		"POST /api/setting/update",
	}

	for _, w := range want {
		if !got[w] {
			t.Errorf("路由未注册: %s", w)
		}
	}
}
