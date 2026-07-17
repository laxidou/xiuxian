package server

import (
	"net/http"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/logging"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"

	worldv1 "xiuxian/gen/go/xiuxian/v1"
	"xiuxian/internal/biz"
	"xiuxian/internal/conf"
	"xiuxian/internal/service"
)

func NewAuxiliaryHTTPHandler(usecase *biz.WorldUsecase, limiter biz.RateLimiter, health biz.DependencyHealthChecker, config *conf.Server) http.Handler {
	return service.NewAuxiliaryHTTPHandler(usecase, limiter, service.AuxiliaryHTTPOptions{
		WorkerToken: config.WorkerToken,
		Version:     config.Version,
	}, health)
}

func NewHTTPServer(config *conf.Server, worldService *service.WorldService, authService *service.AuthService, auxiliary http.Handler, logger log.Logger) *kratoshttp.Server {
	server := kratoshttp.NewServer(
		kratoshttp.Address(config.HTTPAddress),
		kratoshttp.Timeout(config.HTTPTimeout),
		kratoshttp.Middleware(
			recovery.Recovery(),
			logging.Server(logger),
		),
	)
	worldv1.RegisterWorldServiceHTTPServer(server, worldService)
	worldv1.RegisterAuthServiceHTTPServer(server, authService)
	server.HandlePrefix("/", auxiliary)
	return server
}
