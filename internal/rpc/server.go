package rpc

import (
	"github.com/go-kratos/kratos/v2/log"

	"xiuxian/internal/biz"
	"xiuxian/internal/service"
	"xiuxian/internal/world"
)

// Server remains an alias so existing in-process gRPC tests and consumers can
// migrate without a transport contract break.
type Server = service.WorldService

func NewServer(authority *world.Service) *Server {
	return service.NewWorldService(biz.NewWorldUsecase(authority, log.DefaultLogger))
}

var (
	RoleState   = service.RoleState
	WorldBounds = service.WorldBounds
)
