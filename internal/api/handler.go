package api

import (
	"net/http"

	"github.com/go-kratos/kratos/v2/log"

	"xiuxian/internal/biz"
	"xiuxian/internal/service"
	"xiuxian/internal/world"
)

// Options is kept as a compatibility alias for tests and embedders that used
// the pre-Kratos HTTP handler directly.
type Options = service.HTTPOptions

func NewHandler(authority *world.Service, options Options) http.Handler {
	usecase := biz.NewWorldUsecase(authority, log.DefaultLogger)
	return service.NewHTTPHandler(usecase, options)
}
