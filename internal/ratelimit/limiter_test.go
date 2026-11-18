package ratelimit

import (
	"context"
	"testing"
	"time"

	"git.lanjak.net/rufi/dbox/internal/config"
)

func TestNewUsesPlanLimits(t *testing.T) {
	if New(config.Starter).limit != 60 || New(config.Pro).limit != 120 {
		t.Fatal("unexpected plan limits")
	}
}

func TestWaitHonorsCancellationWhenLimitReached(t *testing.T) {
	l := New(config.Starter)
	for i := 0; i < l.limit; i++ {
		l.times = append(l.times, time.Now())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.Wait(ctx); err == nil {
		t.Fatal("expected cancellation")
	}
}
