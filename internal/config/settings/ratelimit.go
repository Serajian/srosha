package settings

import "github.com/Serajian/srosha/pkg/env"

type RateLimit struct {
	PerMinute int
}

func LoadRateLimit(r *env.Reader) RateLimit {
	rl := RateLimit{PerMinute: r.Int("RATELIMIT_PER_MINUTE", 600)}
	r.Check(rl.PerMinute > 0, "NOTIF_RATELIMIT_PER_MINUTE must be above zero")
	return rl
}
