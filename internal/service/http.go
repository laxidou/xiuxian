package service

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

	"xiuxian/internal/biz"
	"xiuxian/internal/rules"
	"xiuxian/internal/world"
)

type HTTPOptions struct {
	AllowTestClock bool
	SecureCookies  bool
	WorkerToken    string
	Version        string
}

type handler struct {
	usecase *biz.WorldUsecase
	options HTTPOptions
	mux     *http.ServeMux
}

func NewHTTPHandler(usecase *biz.WorldUsecase, options HTTPOptions) http.Handler {
	if strings.TrimSpace(options.Version) == "" {
		options.Version = "dev"
	}
	h := &handler{usecase: usecase, options: options, mux: http.NewServeMux()}
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
	token, state, err := h.usecase.Register(r.Context(), input.Account, input.Password, input.RoleName)
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
	token, state, err := h.usecase.Login(r.Context(), input.Account, input.Password)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	h.setSession(w, token)
	writeJSON(w, http.StatusOK, state)
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request, _ string) {
	if cookie, err := r.Cookie("xiuxian_session"); err == nil {
		if err := h.usecase.Logout(r.Context(), cookie.Value); err != nil {
			writeServiceError(w, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "xiuxian_session", Value: "", Path: "/", HttpOnly: true, Secure: h.options.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) state(w http.ResponseWriter, r *http.Request, roleID string) {
	state, err := h.usecase.State(r.Context(), roleID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *handler) generatedState(w http.ResponseWriter, r *http.Request, roleID string) {
	state, err := h.usecase.State(r.Context(), roleID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeProtoJSON(w, RoleState(state))
}

func (h *handler) generatedBounds(w http.ResponseWriter, r *http.Request, _ string) {
	bounds, err := h.usecase.Bounds(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeProtoJSON(w, WorldBounds(bounds))
}

func (h *handler) move(w http.ResponseWriter, r *http.Request, roleID string) {
	var input struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	expectation, ok := h.commandExpectation(w, r, roleID)
	if !ok {
		return
	}
	state, err := h.usecase.Move(r.Context(), roleID, r.Header.Get("Idempotency-Key"), rules.Position{X: rules.Units(input.X), Y: rules.Units(input.Y)}, expectation)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *handler) stop(w http.ResponseWriter, r *http.Request, roleID string) {
	expectation, ok := h.commandExpectation(w, r, roleID)
	if !ok {
		return
	}
	state, err := h.usecase.Stop(r.Context(), roleID, r.Header.Get("Idempotency-Key"), expectation)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *handler) scan(w http.ResponseWriter, r *http.Request, roleID string) {
	expectation, ok := h.commandExpectation(w, r, roleID)
	if !ok {
		return
	}
	result, err := h.usecase.Scan(r.Context(), roleID, expectation)
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
	expectation, ok := h.commandExpectation(w, r, roleID)
	if !ok {
		return
	}
	state, err := h.usecase.Transfer(r.Context(), roleID, input.TargetID, r.Header.Get("Idempotency-Key"), input.AmountMinutes, expectation)
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
	expectation, ok := h.commandExpectation(w, r, roleID)
	if !ok {
		return
	}
	state, err := h.usecase.Seize(r.Context(), roleID, input.TargetID, r.Header.Get("Idempotency-Key"), expectation)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *handler) events(w http.ResponseWriter, r *http.Request, roleID string) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := h.usecase.Events(r.Context(), roleID, after, limit)
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
		events, err := h.usecase.Events(r.Context(), roleID, after, 100)
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

func (h *handler) bounds(w http.ResponseWriter, r *http.Request, _ string) {
	bounds, err := h.usecase.Bounds(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bounds)
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
	expectation, ok := h.commandExpectation(w, r, roleID)
	if !ok {
		return
	}
	state, err := h.usecase.Reincarnate(r.Context(), roleID, r.Header.Get("Idempotency-Key"), position, expectation)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *handler) rotateMCPKey(w http.ResponseWriter, r *http.Request, roleID string) {
	key, err := h.usecase.RotateMCPKey(r.Context(), roleID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"api_key": key})
}

func (h *handler) revokeMCPKey(w http.ResponseWriter, r *http.Request, roleID string) {
	if err := h.usecase.RevokeMCPKey(r.Context(), roleID); err != nil {
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
	expectation, ok := h.commandExpectation(w, r, roleID)
	if !ok {
		return
	}
	conversation, err := h.usecase.RequestConversation(r.Context(), roleID, input.TargetID, r.Header.Get("Idempotency-Key"), expectation)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, conversation)
}

func (h *handler) listConversations(w http.ResponseWriter, r *http.Request, roleID string) {
	conversations, err := h.usecase.Conversations(r.Context(), roleID)
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
	expectation, ok := h.commandExpectation(w, r, roleID)
	if !ok {
		return
	}
	conversation, err := h.usecase.RespondConversation(r.Context(), roleID, r.PathValue("conversation_id"), r.Header.Get("Idempotency-Key"), input.Action, expectation)
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
	expectation, ok := h.commandExpectation(w, r, roleID)
	if !ok {
		return
	}
	message, err := h.usecase.SendConversationMessage(r.Context(), roleID, r.PathValue("conversation_id"), r.Header.Get("Idempotency-Key"), input.Content, expectation)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, message)
}

func (h *handler) closeConversation(w http.ResponseWriter, r *http.Request, roleID string) {
	expectation, ok := h.commandExpectation(w, r, roleID)
	if !ok {
		return
	}
	conversation, err := h.usecase.CloseConversation(r.Context(), roleID, r.PathValue("conversation_id"), r.Header.Get("Idempotency-Key"), expectation)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conversation)
}

func (h *handler) advanceClock(w http.ResponseWriter, r *http.Request) {
	manual, ok := h.usecase.Clock().(*world.ManualClock)
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
	settled, err := h.usecase.SettleDeadline(r.Context(), input.RoleID, input.StateVersion)
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
			roleID, authErr := h.usecase.AuthenticateSession(r.Context(), cookie.Value)
			if authErr == nil {
				next(w, r, roleID)
				return
			}
		}
		if bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); bearer != r.Header.Get("Authorization") {
			roleID, authErr := h.usecase.AuthenticateAPIKey(r.Context(), bearer)
			if authErr == nil {
				next(w, r, roleID)
				return
			}
		}
		writeServiceError(w, biz.ErrUnauthenticated)
	}
}

func (h *handler) commandExpectation(w http.ResponseWriter, r *http.Request, roleID string) (biz.CommandExpectation, bool) {
	lifeNumber, lifeErr := strconv.ParseInt(r.Header.Get("X-Expected-Life-Number"), 10, 64)
	stateVersion, versionErr := strconv.ParseInt(r.Header.Get("X-Expected-State-Version"), 10, 64)
	if lifeErr == nil && versionErr == nil && lifeNumber > 0 && stateVersion > 0 {
		return biz.CommandExpectation{LifeNumber: lifeNumber, StateVersion: stateVersion}, true
	}
	if h.options.AllowTestClock {
		state, err := h.usecase.State(r.Context(), roleID)
		if err != nil {
			writeServiceError(w, err)
			return biz.CommandExpectation{}, false
		}
		return biz.CommandExpectation{LifeNumber: state.LifeNumber, StateVersion: state.StateVersion}, true
	}
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected life number and state version are required"})
	return biz.CommandExpectation{}, false
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
	case errors.Is(err, biz.ErrInvalid), errors.Is(err, biz.ErrIdempotencyKey):
		status, message = http.StatusBadRequest, err.Error()
	case errors.Is(err, biz.ErrConflict):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, biz.ErrUnauthenticated):
		status, message = http.StatusUnauthorized, err.Error()
	case errors.Is(err, biz.ErrNotAlive):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, biz.ErrNotFound):
		status, message = http.StatusNotFound, err.Error()
	case errors.Is(err, biz.ErrForbidden):
		status, message = http.StatusForbidden, err.Error()
	case errors.Is(err, biz.ErrRateLimited):
		status, message = http.StatusTooManyRequests, err.Error()
	case errors.Is(err, biz.ErrStaleCommand):
		status, message = http.StatusConflict, err.Error()
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
