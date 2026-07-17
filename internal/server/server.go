package server

import (
	"github.com/google/wire"

	"xiuxian/internal/conf"
)

var ProviderSet = wire.NewSet(
	conf.ProvideServer,
	NewLegacyHTTPHandler,
	NewHTTPServer,
	NewGRPCServer,
)
