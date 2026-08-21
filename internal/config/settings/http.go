package settings

import "github.com/Serajian/srosha/pkg/env"

// HTTP is the dispatcher's own listener, which serves nothing but /healthz.
type HTTP struct {
	Addr string
}

func LoadHTTP(r *env.Reader) HTTP {
	return HTTP{Addr: r.Str("HTTP_ADDR", ":8081")}
}
