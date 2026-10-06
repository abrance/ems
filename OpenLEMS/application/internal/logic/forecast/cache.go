package forecast

import (
	"context"
	"sync"
	"time"

	"github.com/gogf/gf/v2/os/gcache"
)

// 预测结果缓存。
//
// 为什么必须缓存：TimesFM 是 CPU 推理，单次约 2s，服务端 max_concurrency=2、
// queue_timeout=60s。而同一份结果会被前端多个面板共用（决策支持 / 长周期电价预测 /
// 预测复盘 / 效果评估），甚至同一屏重复拉。不缓存的话切几个 Tab 就是几次推理，
// 两个人同时用就开始排队。
//
// 5 分钟量级对预测结果是安全的：序列本身是分钟/日级采样，模型一天也只重训一次。
const forecastCacheDuration = 5 * time.Minute

var (
	forecastCache     *gcache.Cache
	forecastCacheOnce sync.Once
)

func forecastCacheInstance() *gcache.Cache {
	forecastCacheOnce.Do(func() { forecastCache = gcache.New() })
	return forecastCache
}

// withForecastCache 命中缓存直接返回，否则调用 produce 并写入。
// force=true 跳过读取（仍写入），供前端手动刷新穿透。
func withForecastCache[T any](
	ctx context.Context,
	key string,
	force bool,
	produce func(context.Context) (*T, error),
) (*T, error) {
	c := forecastCacheInstance()
	if !force {
		if v, err := c.Get(ctx, key); err == nil && v != nil && !v.IsNil() {
			// gcache 存的是 *gvar.Var，取出原始值再断言
			if hit, ok := v.Val().(*T); ok {
				return hit, nil
			}
		}
	}
	out, err := produce(ctx)
	if err != nil {
		return nil, err
	}
	// 写缓存失败不影响本次结果，忽略错误
	_ = c.Set(ctx, key, out, forecastCacheDuration)
	return out, nil
}
