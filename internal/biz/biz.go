package biz

import "github.com/google/wire"

// ProviderSet contains the world authority and use-case providers.
var ProviderSet = wire.NewSet(
	NewWorldAuthority,
	wire.Bind(new(WorldAuthority), new(*Service)),
	NewWorldUsecase,
)
