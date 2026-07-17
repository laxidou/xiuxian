package main

import (
	"os"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/log"
	kratosgrpc "github.com/go-kratos/kratos/v2/transport/grpc"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"

	"xiuxian/internal/conf"
)

const serviceName = "xiuxian.game-server"

func main() {
	config := conf.Load()
	baseLogger := log.NewFilter(
		log.NewStdLogger(os.Stdout),
		log.FilterKey("args"),
	)
	logger := log.With(
		baseLogger,
		"ts", log.DefaultTimestamp,
		"caller", log.DefaultCaller,
		"service.name", serviceName,
		"service.version", config.Server.Version,
	)
	app, cleanup, err := wireApp(config, logger)
	if err != nil {
		log.NewHelper(logger).Fatal(err)
	}
	defer cleanup()
	if err := app.Run(); err != nil {
		log.NewHelper(logger).Fatal(err)
	}
}

func newApp(config *conf.Config, logger log.Logger, httpServer *kratoshttp.Server, grpcServer *kratosgrpc.Server) *kratos.App {
	return kratos.New(
		kratos.Name(serviceName),
		kratos.Version(config.Server.Version),
		kratos.Logger(logger),
		kratos.Server(httpServer, grpcServer),
	)
}
