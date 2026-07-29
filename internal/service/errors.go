package service

import (
	"fmt"
	"net/http"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
)

// xiuxian.v1 error reasons.  These strings are the machine-readable contract
// used by HTTP and gRPC clients to classify failures.
const (
	ReasonInvalidCommand        = "INVALID_COMMAND"
	ReasonTargetRequired        = "TARGET_REQUIRED"
	ReasonAuthRequired          = "AUTH_REQUIRED"
	ReasonNotFound              = "NOT_FOUND"
	ReasonForbidden             = "FORBIDDEN"
	ReasonStaleOrConflictingCmd = "STALE_OR_CONFLICTING_COMMAND"
	ReasonRateLimited           = "RATE_LIMITED"
	ReasonRateLimitUnavailable  = "RATE_LIMIT_UNAVAILABLE"
	ReasonInternal              = "INTERNAL"
)

// ErrorBadRequest builds a 400 BadRequest with reason ReasonInvalidCommand.
func ErrorBadRequest(format string, a ...any) *kratoserrors.Error {
	return kratoserrors.BadRequest(ReasonInvalidCommand, fmt.Sprintf(format, a...))
}

// ErrorTargetRequired builds a 400 BadRequest when a required target field is missing.
func ErrorTargetRequired(format string, a ...any) *kratoserrors.Error {
	return kratoserrors.BadRequest(ReasonTargetRequired, fmt.Sprintf(format, a...))
}

// ErrorUnauthorized builds a 401 Unauthorized.
func ErrorUnauthorized(format string, a ...any) *kratoserrors.Error {
	return kratoserrors.Unauthorized(ReasonAuthRequired, fmt.Sprintf(format, a...))
}

// ErrorNotFound builds a 404 NotFound.
func ErrorNotFound(format string, a ...any) *kratoserrors.Error {
	return kratoserrors.NotFound(ReasonNotFound, fmt.Sprintf(format, a...))
}

// ErrorForbidden builds a 403 Forbidden.
func ErrorForbidden(format string, a ...any) *kratoserrors.Error {
	return kratoserrors.Forbidden(ReasonForbidden, fmt.Sprintf(format, a...))
}

// ErrorPreconditionFailed builds a 412 PreconditionFailed for stale or conflicting commands.
func ErrorPreconditionFailed(format string, a ...any) *kratoserrors.Error {
	return kratoserrors.New(http.StatusPreconditionFailed, ReasonStaleOrConflictingCmd, fmt.Sprintf(format, a...))
}

// ErrorRateLimited builds a 429 TooManyRequests.
func ErrorRateLimited(format string, a ...any) *kratoserrors.Error {
	return kratoserrors.New(http.StatusTooManyRequests, ReasonRateLimited, fmt.Sprintf(format, a...))
}

// ErrorRateLimitUnavailable builds a 503 ServiceUnavailable when the rate-limiter backend fails.
func ErrorRateLimitUnavailable(format string, a ...any) *kratoserrors.Error {
	return kratoserrors.ServiceUnavailable(ReasonRateLimitUnavailable, fmt.Sprintf(format, a...))
}

// ErrorInternal builds a 500 InternalServerError.
func ErrorInternal(format string, a ...any) *kratoserrors.Error {
	return kratoserrors.InternalServer(ReasonInternal, fmt.Sprintf(format, a...))
}
