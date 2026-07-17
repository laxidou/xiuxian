//go:build wireinject

package main

import (
	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"

	"xiuxian/internal/biz"
	"xiuxian/internal/conf"
	"xiuxian/internal/data"
	"xiuxian/internal/server"
	"xiuxian/internal/service"
)

func wireApp(*conf.Config, log.Logger) (*kratos.App, func(), error) {
	panic(wire.Build(
		data.ProviderSet,
		biz.ProviderSet,
		service.ProviderSet,
		server.ProviderSet,
		newApp,
	))
}
