package forecast

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/pkg/errors"
)

func TestWithForecastCacheHit(t *testing.T) {
	ctx := context.Background()
	var calls int32
	produce := func(context.Context) (*SLoadForecast, error) {
		atomic.AddInt32(&calls, 1)
		return &SLoadForecast{DeviceId: "d1", Model: "m"}, nil
	}

	first, err := withForecastCache(ctx, "test:hit:1", false, produce)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := withForecastCache(ctx, "test:hit:1", false, produce)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("第二次应命中缓存，实际推理 %d 次", calls)
	}
	if first.DeviceId != second.DeviceId || second.DeviceId != "d1" {
		t.Errorf("缓存内容不一致: %+v / %+v", first, second)
	}
}

func TestWithForecastCacheForceRefresh(t *testing.T) {
	ctx := context.Background()
	var calls int32
	produce := func(context.Context) (*SPriceForecast, error) {
		n := atomic.AddInt32(&calls, 1)
		return &SPriceForecast{Market: "ahead", ContextPoints: int(n)}, nil
	}

	if _, err := withForecastCache(ctx, "test:force:1", false, produce); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := withForecastCache(ctx, "test:force:1", true, produce)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Errorf("force 应穿透缓存，实际推理 %d 次", calls)
	}
	if got.ContextPoints != 2 {
		t.Errorf("force 后应拿到新结果, got %+v", got)
	}
}

func TestWithForecastCacheErrorNotCached(t *testing.T) {
	ctx := context.Background()
	var calls int32
	produce := func(context.Context) (*SPriceForecast, error) {
		atomic.AddInt32(&calls, 1)
		return nil, errors.New("boom")
	}

	for i := 0; i < 2; i++ {
		if _, err := withForecastCache(ctx, "test:err:1", false, produce); err == nil {
			t.Fatal("应返回错误")
		}
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Errorf("失败结果不应入缓存，实际推理 %d 次", calls)
	}
}
