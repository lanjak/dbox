package ratelimit

import (
	"context"
	"sync"
	"time"

	"git.lanjak.net/rufi/dbox/internal/config"
)

type Limiter struct {
	mu    sync.Mutex
	limit int
	times []time.Time
}

func New(plan config.Plan) *Limiter {
	limit := 60
	if plan == config.Pro {
		limit = 120
	}
	return &Limiter{limit: limit}
}

func (l *Limiter) Wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		now := time.Now()
		cutoff := now.Add(-time.Minute)
		i := 0
		for i < len(l.times) && l.times[i].Before(cutoff) {
			i++
		}
		l.times = l.times[i:]
		if len(l.times) < l.limit {
			l.times = append(l.times, now)
			l.mu.Unlock()
			return nil
		}
		wait := time.Until(l.times[0].Add(time.Minute))
		l.mu.Unlock()
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}
