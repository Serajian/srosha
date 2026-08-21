package settings

import "github.com/Serajian/srosha/pkg/env"

type GRPC struct {
	Addr     string
	HTTPAddr string
}

func LoadGRPC(r *env.Reader) GRPC {
	return GRPC{
		Addr:     r.Str("GRPC_ADDR", ":50051"),
		HTTPAddr: r.Str("GRPC_HTTP_ADDR", ":8080"),
	}
}
