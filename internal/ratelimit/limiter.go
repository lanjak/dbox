package ratelimit

import "github.com/rufi/dbox/internal/config"

type Limiter struct{}

func New(plan config.Plan) *Limiter {
	return &Limiter{}
}
