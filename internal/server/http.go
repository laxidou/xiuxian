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

func NewLegacyHTTPHandler(usecase *biz.WorldUsecase, config *conf.Server) http.Handler {
	return service.NewHTTPHandler(usecase, service.HTTPOptions{
		SecureCookies: config.SecureCookie,
		WorkerToken:   config.WorkerToken,
		Version:       config.Version,
	})
}

func NewHTTPServer(config *conf.Server, worldService *service.WorldService, legacy http.Handler, logger log.Logger) *kratoshttp.Server {
	server := kratoshttp.NewServer(
		kratoshttp.Address(config.HTTPAddress),
		kratoshttp.Timeout(config.HTTPTimeout),
		kratoshttp.Middleware(
			recovery.Recovery(),
			logging.Server(logger),
		),
	)
	worldv1.RegisterWorldServiceHTTPServer(server, worldService)
	server.HandlePrefix("/", legacy)
	return server
}
