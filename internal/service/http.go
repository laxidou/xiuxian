package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"xiuxian/internal/biz"
)

type AuxiliaryHTTPOptions struct {
	AllowTestClock    bool
	DisableRateLimits bool
	WorkerToken       string
	Version           string
}

type auxiliaryHandler struct {
	usecase *biz.WorldUsecase
	limiter biz.RateLimiter
	health  biz.DependencyHealthChecker
	options AuxiliaryHTTPOptions
	mux     *http.ServeMux
}

func NewAuxiliaryHTTPHandler(usecase *biz.WorldUsecase, limiter biz.RateLimiter, options AuxiliaryHTTPOptions, healthCheckers ...biz.DependencyHealthChecker) http.Handler {
	if strings.TrimSpace(options.Version) == "" {
		options.Version = "dev"
	}
	handler := &auxiliaryHandler{usecase: usecase, limiter: limiter, options: options, mux: http.NewServeMux()}
	if len(healthCheckers) > 0 {
		handler.health = healthCheckers[0]
	}
	handler.mux.HandleFunc("GET /healthz", handler.healthz)
	handler.mux.HandleFunc("POST /internal/deadlines/settle", handler.settleDeadline)
	handler.mux.HandleFunc("GET /events/stream", handler.auth(handler.eventsStream))
	if options.AllowTestClock {
		handler.mux.HandleFunc("POST /test/clock/advance", handler.advanceClock)
	}
	return securityHeaders(handler.mux)
}

func (handler *auxiliaryHandler) healthz(w http.ResponseWriter, r *http.Request) {
	response := map[string]string{
		"status": "ok", "service": "game-server", "version": handler.options.Version,
		"postgres": "disabled", "redis": "disabled",
	}
	statusCode := http.StatusOK
	if handler.health != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		dependencies := handler.health.Health(ctx)
		response["postgres"] = dependencies.Postgres
		response["redis"] = dependencies.Redis
		switch {
		case dependencies.Postgres == "unavailable":
			response["status"] = "unavailable"
			statusCode = http.StatusServiceUnavailable
		case dependencies.Redis == "unavailable":
			response["status"] = "unavailable"
			statusCode = http.StatusServiceUnavailable
		}
	}
	writeJSON(w, statusCode, response)
}

func (handler *auxiliaryHandler) eventsStream(w http.ResponseWriter, r *http.Request, roleID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unavailable"})
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	for {
		events, err := handler.usecase.Events(r.Context(), roleID, after, 100)
		if err != nil {
			return
		}
		for _, event := range events {
			payload, _ := json.Marshal(event)
			_, _ = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, event.Type, payload)
			after = event.ID
		}
		flusher.Flush()
		if r.URL.Query().Get("once") == "1" {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(2 * time.Second):
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (handler *auxiliaryHandler) advanceClock(w http.ResponseWriter, r *http.Request) {
	manual, ok := handler.usecase.Clock().(*biz.ManualClock)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "test clock unavailable"})
		return
	}
	milliseconds, err := strconv.ParseInt(r.URL.Query().Get("milliseconds"), 10, 64)
	if err != nil || milliseconds < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid milliseconds"})
		return
	}
	manual.Advance(time.Duration(milliseconds) * time.Millisecond)
	writeJSON(w, http.StatusOK, map[string]int64{"now": manual.Now().UnixMilli()})
}

func (handler *auxiliaryHandler) settleDeadline(w http.ResponseWriter, r *http.Request) {
	provided := r.Header.Get("X-Worker-Token")
	if handler.options.WorkerToken == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(handler.options.WorkerToken)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "worker authentication required"})
		return
	}
	var input struct {
		RoleID       string `json:"role_id"`
		StateVersion int64  `json:"state_version"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	settled, err := handler.usecase.SettleDeadline(r.Context(), input.RoleID, input.StateVersion)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"settled": settled})
}

func (handler *auxiliaryHandler) auth(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie("xiuxian_session"); err == nil {
			roleID, authErr := handler.usecase.AuthenticateSession(r.Context(), cookie.Value)
			if authErr == nil {
				if handler.allow(w, r, "web_session", cookie.Value, biz.WebSessionRateLimit) {
					next(w, r, roleID)
				}
				return
			}
		}
		if bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); bearer != r.Header.Get("Authorization") {
			roleID, authErr := handler.usecase.AuthenticateAPIKey(r.Context(), bearer)
			if authErr == nil {
				if handler.allow(w, r, "api_key", bearer, biz.APIKeyRateLimit) {
					next(w, r, roleID)
				}
				return
			}
		}
		writeServiceError(w, biz.ErrUnauthenticated)
	}
}

func (handler *auxiliaryHandler) allow(w http.ResponseWriter, r *http.Request, scope, subject string, policy biz.RateLimitPolicy) bool {
	if handler.options.DisableRateLimits {
		return true
	}
	allowed, err := handler.limiter.Allow(r.Context(), scope, subject, policy)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "rate limiter unavailable"})
		return false
	}
	if !allowed {
		writeServiceError(w, biz.ErrRateLimited)
		return false
	}
	return true
}

func clientAddress(r *http.Request) string {
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); forwarded != "" {
		return forwarded
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return false
	}
	return true
}

func writeServiceError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := "internal error"
	switch {
	case errors.Is(err, biz.ErrInvalid), errors.Is(err, biz.ErrIdempotencyKey):
		status, message = http.StatusBadRequest, err.Error()
	case errors.Is(err, biz.ErrUnauthenticated):
		status, message = http.StatusUnauthorized, err.Error()
	case errors.Is(err, biz.ErrNotFound):
		status, message = http.StatusNotFound, err.Error()
	case errors.Is(err, biz.ErrRateLimited):
		status, message = http.StatusTooManyRequests, err.Error()
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
