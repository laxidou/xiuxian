package server

import (
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/logging"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	kratosgrpc "github.com/go-kratos/kratos/v2/transport/grpc"

	worldv1 "xiuxian/gen/go/xiuxian/v1"
	"xiuxian/internal/conf"
	"xiuxian/internal/service"
)

func NewGRPCServer(config *conf.Server, worldService *service.WorldService, logger log.Logger) *kratosgrpc.Server {
	server := kratosgrpc.NewServer(
		kratosgrpc.Address(config.GRPCAddress),
		kratosgrpc.Timeout(config.GRPCTimeout),
		kratosgrpc.Middleware(
			recovery.Recovery(),
			logging.Server(logger),
		),
	)
	worldv1.RegisterWorldServiceServer(server, worldService)
	return server
}
