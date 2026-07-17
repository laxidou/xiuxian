package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	worldrpc "xiuxian/internal/rpc"
	"xiuxian/internal/rules"
	"xiuxian/internal/world"
)

type Options struct {
	AllowTestClock bool
	SecureCookies  bool
	WorkerToken    string
	Version        string
}

type handler struct {
	service *world.Service
	options Options
	mux     *http.ServeMux
}

func NewHandler(service *world.Service, options Options) http.Handler {
	if strings.TrimSpace(options.Version) == "" {
		options.Version = "dev"
	}
	h := &handler{service: service, options: options, mux: http.NewServeMux()}
	health := func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "game-server", "version": options.Version})
	}
	h.mux.HandleFunc("GET /healthz", health)
	h.mux.HandleFunc("GET /api/v1/healthz", health)
	h.mux.HandleFunc("POST /internal/deadlines/settle", h.settleDeadline)
	h.mux.HandleFunc("POST /xiuxian.v1.WorldService/GetState", h.auth(h.generatedState))
	h.mux.HandleFunc("POST /xiuxian.v1.WorldService/GetWorldBounds", h.auth(h.generatedBounds))
	h.mux.HandleFunc("POST /api/v1/auth/register", h.register)
	h.mux.HandleFunc("POST /api/v1/auth/login", h.login)
	h.mux.HandleFunc("POST /api/v1/auth/logout", h.auth(h.logout))
	h.mux.HandleFunc("GET /api/v1/state", h.auth(h.state))
	h.mux.HandleFunc("POST /api/v1/movement/move", h.auth(h.move))
	h.mux.HandleFunc("POST /api/v1/movement/stop", h.auth(h.stop))
	h.mux.HandleFunc("POST /api/v1/scan", h.auth(h.scan))
	h.mux.HandleFunc("POST /api/v1/cultivation/transfer", h.auth(h.transfer))
	h.mux.HandleFunc("POST /api/v1/cultivation/seize", h.auth(h.seize))
	h.mux.HandleFunc("GET /api/v1/events", h.auth(h.events))
	h.mux.HandleFunc("GET /api/v1/events/stream", h.auth(h.eventsStream))
	h.mux.HandleFunc("GET /api/v1/world/bounds", h.auth(h.bounds))
	h.mux.HandleFunc("POST /api/v1/reincarnate", h.auth(h.reincarnate))
	h.mux.HandleFunc("POST /api/v1/mcp-key/rotate", h.auth(h.rotateMCPKey))
	h.mux.HandleFunc("POST /api/v1/mcp-key/revoke", h.auth(h.revokeMCPKey))
	h.mux.HandleFunc("POST /api/v1/conversations", h.auth(h.requestConversation))
	h.mux.HandleFunc("GET /api/v1/conversations", h.auth(h.listConversations))
	h.mux.HandleFunc("POST /api/v1/conversations/{conversation_id}/respond", h.auth(h.respondConversation))
	h.mux.HandleFunc("POST /api/v1/conversations/{conversation_id}/messages", h.auth(h.sendConversationMessage))
	h.mux.HandleFunc("POST /api/v1/conversations/{conversation_id}/close", h.auth(h.closeConversation))
	if options.AllowTestClock {
		h.mux.HandleFunc("POST /api/v1/test/clock/advance", h.advanceClock)
	}
	return securityHeaders(h.mux)
}

func (h *handler) register(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Account  string `json:"account"`
		Password string `json:"password"`
		RoleName string `json:"role_name"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	token, state, err := h.service.Register(input.Account, input.Password, input.RoleName)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	h.setSession(w, token)
	writeJSON(w, http.StatusCreated, state)
}

func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Account  string `json:"account"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	token, state, err := h.service.Login(input.Account, input.Password)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	h.setSession(w, token)
	writeJSON(w, http.StatusOK, state)
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request, _ string) {
	if cookie, err := r.Cookie("xiuxian_session"); err == nil {
		if err := h.service.Logout(cookie.Value); err != nil {
			writeServiceError(w, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "xiuxian_session", Value: "", Path: "/", HttpOnly: true, Secure: h.options.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) state(w http.ResponseWriter, _ *http.Request, roleID string) {
	state, err := h.service.State(roleID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *handler) generatedState(w http.ResponseWriter, _ *http.Request, roleID string) {
	state, err := h.service.State(roleID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeProtoJSON(w, worldrpc.RoleState(state))
}

func (h *handler) generatedBounds(w http.ResponseWriter, _ *http.Request, _ string) {
	writeProtoJSON(w, worldrpc.WorldBounds(h.service.Bounds()))
}

func (h *handler) move(w http.ResponseWriter, r *http.Request, roleID string) {
	var input struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	state, err := h.service.Move(roleID, r.Header.Get("Idempotency-Key"), rules.Position{X: rules.Units(input.X), Y: rules.Units(input.Y)})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *handler) stop(w http.ResponseWriter, r *http.Request, roleID string) {
	state, err := h.service.Stop(roleID, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *handler) scan(w http.ResponseWriter, _ *http.Request, roleID string) {
	result, err := h.service.Scan(roleID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *handler) transfer(w http.ResponseWriter, r *http.Request, roleID string) {
	var input struct {
		TargetID      string `json:"target_id"`
		AmountMinutes int64  `json:"amount_minutes"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	state, err := h.service.Transfer(roleID, input.TargetID, r.Header.Get("Idempotency-Key"), input.AmountMinutes)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *handler) seize(w http.ResponseWriter, r *http.Request, roleID string) {
	var input struct {
		TargetID string `json:"target_id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	state, err := h.service.Seize(roleID, input.TargetID, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *handler) events(w http.ResponseWriter, r *http.Request, roleID string) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := h.service.Events(roleID, after, limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (h *handler) eventsStream(w http.ResponseWriter, r *http.Request, roleID string) {
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
		events, err := h.service.Events(roleID, after, 100)
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

func (h *handler) bounds(w http.ResponseWriter, _ *http.Request, _ string) {
	writeJSON(w, http.StatusOK, h.service.Bounds())
}

func (h *handler) reincarnate(w http.ResponseWriter, r *http.Request, roleID string) {
	var input struct {
		X      *float64 `json:"x"`
		Y      *float64 `json:"y"`
		Random bool     `json:"random"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	var position *rules.Position
	if !input.Random {
		if input.X == nil || input.Y == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "x and y are required unless random is true"})
			return
		}
		value := rules.Position{X: rules.Units(*input.X), Y: rules.Units(*input.Y)}
		position = &value
	}
	state, err := h.service.Reincarnate(roleID, r.Header.Get("Idempotency-Key"), position)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *handler) rotateMCPKey(w http.ResponseWriter, _ *http.Request, roleID string) {
	key, err := h.service.RotateMCPKey(roleID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"api_key": key})
}

func (h *handler) revokeMCPKey(w http.ResponseWriter, _ *http.Request, roleID string) {
	if err := h.service.RevokeMCPKey(roleID); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) requestConversation(w http.ResponseWriter, r *http.Request, roleID string) {
	var input struct {
		TargetID string `json:"target_id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	conversation, err := h.service.RequestConversation(roleID, input.TargetID, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, conversation)
}

func (h *handler) listConversations(w http.ResponseWriter, _ *http.Request, roleID string) {
	conversations, err := h.service.Conversations(roleID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": conversations})
}

func (h *handler) respondConversation(w http.ResponseWriter, r *http.Request, roleID string) {
	var input struct {
		Action string `json:"action"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	conversation, err := h.service.RespondConversation(roleID, r.PathValue("conversation_id"), r.Header.Get("Idempotency-Key"), input.Action)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conversation)
}

func (h *handler) sendConversationMessage(w http.ResponseWriter, r *http.Request, roleID string) {
	var input struct {
		Content string `json:"content"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	message, err := h.service.SendConversationMessage(roleID, r.PathValue("conversation_id"), r.Header.Get("Idempotency-Key"), input.Content)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, message)
}

func (h *handler) closeConversation(w http.ResponseWriter, r *http.Request, roleID string) {
	conversation, err := h.service.CloseConversation(roleID, r.PathValue("conversation_id"), r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conversation)
}

func (h *handler) advanceClock(w http.ResponseWriter, r *http.Request) {
	manual, ok := h.service.Clock().(*world.ManualClock)
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

func (h *handler) settleDeadline(w http.ResponseWriter, r *http.Request) {
	provided := r.Header.Get("X-Worker-Token")
	if h.options.WorkerToken == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(h.options.WorkerToken)) != 1 {
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
	settled, err := h.service.SettleDeadline(input.RoleID, input.StateVersion)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"settled": settled})
}

func (h *handler) auth(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("xiuxian_session")
		if err == nil {
			roleID, authErr := h.service.AuthenticateSession(cookie.Value)
			if authErr == nil {
				next(w, r, roleID)
				return
			}
		}
		if bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); bearer != r.Header.Get("Authorization") {
			roleID, authErr := h.service.AuthenticateAPIKey(bearer)
			if authErr == nil {
				next(w, r, roleID)
				return
			}
		}
		writeServiceError(w, world.ErrUnauthenticated)
	}
}

func (h *handler) setSession(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "xiuxian_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.options.SecureCookies,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int((24 * time.Hour).Seconds()),
	})
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
	case errors.Is(err, world.ErrInvalid), errors.Is(err, world.ErrIdempotencyKey):
		status, message = http.StatusBadRequest, err.Error()
	case errors.Is(err, world.ErrConflict):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, world.ErrUnauthenticated):
		status, message = http.StatusUnauthorized, err.Error()
	case errors.Is(err, world.ErrNotAlive):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, world.ErrNotFound):
		status, message = http.StatusNotFound, err.Error()
	case errors.Is(err, world.ErrForbidden):
		status, message = http.StatusForbidden, err.Error()
	case errors.Is(err, world.ErrRateLimited):
		status, message = http.StatusTooManyRequests, err.Error()
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeProtoJSON(w http.ResponseWriter, message proto.Message) {
	payload, err := protojson.Marshal(message)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encode contract response"})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
