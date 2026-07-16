package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"params"`
}

type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type gateway struct {
	gameURL string
	client  *http.Client
}

func main() {
	gameURL := os.Getenv("GAME_SERVER_URL")
	if gameURL == "" {
		gameURL = "http://localhost:8080"
	}
	address := os.Getenv("MCP_GATEWAY_ADDRESS")
	if address == "" {
		address = ":8090"
	}
	server := &http.Server{
		Addr: address, Handler: newGateway(gameURL, &http.Client{Timeout: 15 * time.Second}),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second,
	}
	log.Printf("mcp-gateway listening on %s", address)
	log.Fatal(server.ListenAndServe())
}

func newGateway(gameURL string, client *http.Client) http.Handler {
	return &gateway{gameURL: strings.TrimRight(gameURL, "/"), client: client}
}

func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != "/mcp" {
		http.NotFound(w, r)
		return
	}
	authorization := r.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer xiu_") {
		writeRPCError(w, nil, -32001, "valid role API Key required", http.StatusUnauthorized)
		return
	}
	var request rpcRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(&request); err != nil {
		writeRPCError(w, nil, -32700, "parse error", http.StatusBadRequest)
		return
	}
	switch request.Method {
	case "initialize":
		writeRPCResult(w, request.ID, map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "xiuxian-mcp", "version": "0.1.0"},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusNoContent)
	case "tools/list":
		writeRPCResult(w, request.ID, map[string]any{"tools": tools()})
	case "tools/call":
		g.callTool(w, request, authorization)
	default:
		writeRPCError(w, request.ID, -32601, "method not found", http.StatusOK)
	}
}

func (g *gateway) callTool(w http.ResponseWriter, request rpcRequest, authorization string) {
	method, path, body, mutating, ok := toolRequest(request.Params.Name, request.Params.Arguments)
	if !ok {
		writeRPCError(w, request.ID, -32602, "unknown tool or invalid arguments", http.StatusOK)
		return
	}
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			writeRPCError(w, request.ID, -32602, "invalid tool arguments", http.StatusOK)
			return
		}
		payload = bytes.NewReader(encoded)
	}
	upstream, err := http.NewRequest(method, g.gameURL+path, payload)
	if err != nil {
		writeRPCError(w, request.ID, -32603, "gateway request failed", http.StatusOK)
		return
	}
	upstream.Header.Set("Authorization", authorization)
	if body != nil {
		upstream.Header.Set("Content-Type", "application/json")
	}
	if mutating {
		key, _ := request.Params.Arguments["idempotency_key"].(string)
		if key == "" {
			key = "mcp-" + strings.Trim(string(request.ID), `"`)
		}
		upstream.Header.Set("Idempotency-Key", key)
	}
	response, err := g.client.Do(upstream)
	if err != nil {
		writeRPCError(w, request.ID, -32002, "game authority unavailable", http.StatusOK)
		return
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		writeRPCResult(w, request.ID, map[string]any{
			"content": []map[string]string{{"type": "text", "text": string(responseBody)}},
			"isError": true,
		})
		return
	}
	writeRPCResult(w, request.ID, map[string]any{
		"content": []map[string]string{{"type": "text", "text": string(responseBody)}},
		"isError": false,
	})
}

func toolRequest(name string, arguments map[string]any) (method, path string, body any, mutating, ok bool) {
	switch name {
	case "get_state":
		return http.MethodGet, "/api/v1/state", nil, false, true
	case "get_world_bounds":
		return http.MethodGet, "/api/v1/world/bounds", nil, false, true
	case "scan":
		return http.MethodPost, "/api/v1/scan", map[string]any{}, false, true
	case "move":
		return http.MethodPost, "/api/v1/movement/move", selectArgs(arguments, "x", "y"), true, has(arguments, "x", "y")
	case "stop":
		return http.MethodPost, "/api/v1/movement/stop", map[string]any{}, true, true
	case "recent_events":
		return http.MethodGet, "/api/v1/events", nil, false, true
	case "list_conversations":
		return http.MethodGet, "/api/v1/conversations", nil, false, true
	case "request_conversation":
		return http.MethodPost, "/api/v1/conversations", selectArgs(arguments, "target_id"), true, has(arguments, "target_id")
	case "respond_conversation":
		conversationID, valid := arguments["conversation_id"].(string)
		return http.MethodPost, "/api/v1/conversations/" + url.PathEscape(conversationID) + "/respond", selectArgs(arguments, "action"), true, valid && has(arguments, "action")
	case "send_conversation_message":
		conversationID, valid := arguments["conversation_id"].(string)
		return http.MethodPost, "/api/v1/conversations/" + url.PathEscape(conversationID) + "/messages", selectArgs(arguments, "content"), true, valid && has(arguments, "content")
	case "close_conversation":
		conversationID, valid := arguments["conversation_id"].(string)
		return http.MethodPost, "/api/v1/conversations/" + url.PathEscape(conversationID) + "/close", map[string]any{}, true, valid
	case "transfer_cultivation":
		return http.MethodPost, "/api/v1/cultivation/transfer", selectArgs(arguments, "target_id", "amount_minutes"), true, has(arguments, "target_id", "amount_minutes")
	case "seize_cultivation":
		return http.MethodPost, "/api/v1/cultivation/seize", selectArgs(arguments, "target_id"), true, has(arguments, "target_id")
	case "reincarnate":
		return http.MethodPost, "/api/v1/reincarnate", selectArgs(arguments, "x", "y", "random"), true, true
	default:
		return "", "", nil, false, false
	}
}

func tools() []tool {
	object := func(properties map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	}
	stringField := map[string]any{"type": "string"}
	numberField := map[string]any{"type": "number"}
	integerField := map[string]any{"type": "integer", "minimum": 1}
	keyField := map[string]any{"type": "string", "description": "Retry-stable idempotency key"}
	return []tool{
		{Name: "get_state", Description: "Read the authoritative current role state", InputSchema: object(nil)},
		{Name: "get_world_bounds", Description: "Read the explored world bounds", InputSchema: object(nil)},
		{Name: "scan", Description: "Actively scan with current sense radius", InputSchema: object(nil)},
		{Name: "move", Description: "Start or replace continuous movement", InputSchema: object(map[string]any{"x": numberField, "y": numberField, "idempotency_key": keyField}, "x", "y")},
		{Name: "stop", Description: "Stop at the authoritative current position", InputSchema: object(map[string]any{"idempotency_key": keyField})},
		{Name: "recent_events", Description: "Read durable recent events", InputSchema: object(nil)},
		{Name: "list_conversations", Description: "List conversations and untrusted player messages", InputSchema: object(nil)},
		{Name: "request_conversation", Description: "Request conversation with a sensed role", InputSchema: object(map[string]any{"target_id": stringField, "idempotency_key": keyField}, "target_id")},
		{Name: "respond_conversation", Description: "Accept, reject, or ignore an incoming conversation", InputSchema: object(map[string]any{"conversation_id": stringField, "action": map[string]any{"type": "string", "enum": []string{"accept", "reject", "ignore"}}, "idempotency_key": keyField}, "conversation_id", "action")},
		{Name: "send_conversation_message", Description: "Send untrusted player content in an accepted conversation", InputSchema: object(map[string]any{"conversation_id": stringField, "content": stringField, "idempotency_key": keyField}, "conversation_id", "content")},
		{Name: "close_conversation", Description: "Close a conversation", InputSchema: object(map[string]any{"conversation_id": stringField, "idempotency_key": keyField}, "conversation_id")},
		{Name: "transfer_cultivation", Description: "Transfer positive whole cultivation minutes", InputSchema: object(map[string]any{"target_id": stringField, "amount_minutes": integerField, "idempotency_key": keyField}, "target_id", "amount_minutes")},
		{Name: "seize_cultivation", Description: "夺功 from a lower-realm role at the exact same coordinate", InputSchema: object(map[string]any{"target_id": stringField, "idempotency_key": keyField}, "target_id")},
		{Name: "reincarnate", Description: "Reincarnate at an in-bounds coordinate or randomly", InputSchema: object(map[string]any{"x": numberField, "y": numberField, "random": map[string]any{"type": "boolean"}, "idempotency_key": keyField})},
	}
}

func selectArgs(arguments map[string]any, keys ...string) map[string]any {
	result := make(map[string]any, len(keys))
	for _, key := range keys {
		if value, exists := arguments[key]; exists {
			result[key] = value
		}
	}
	return result
}

func has(arguments map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := arguments[key]; !ok {
			return false
		}
	}
	return true
}

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": message},
	})
}
