package main

import (
	"context"
	"encoding/json"
	stdlog "log"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	kratoslog "github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	worldv1 "xiuxian/gen/go/xiuxian/v1"
	"xiuxian/internal/biz"
	"xiuxian/internal/data"
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
	rpc    worldv1.WorldServiceClient
	health grpc_health_v1.HealthClient
	limits biz.RateLimiter
}

func main() {
	address := os.Getenv("MCP_GATEWAY_ADDRESS")
	if address == "" {
		address = ":8090"
	}
	grpcAddress := os.Getenv("GAME_SERVER_GRPC_ADDRESS")
	if grpcAddress == "" {
		grpcAddress = "localhost:9090"
	}
	connection, err := grpc.NewClient(grpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		stdlog.Fatal(err)
	}
	defer connection.Close()
	limitContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	limiter, cleanupLimiter, err := data.OpenRedisRateLimiter(
		limitContext,
		os.Getenv("REDIS_URL"),
		kratoslog.NewStdLogger(os.Stdout),
	)
	cancel()
	if err != nil {
		stdlog.Fatal(err)
	}
	defer cleanupLimiter()
	server := &http.Server{
		Addr: address, Handler: newRPCGatewayWithLimiter(worldv1.NewWorldServiceClient(connection), limiter, grpc_health_v1.NewHealthClient(connection)),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second,
	}
	stdlog.Printf("mcp-gateway listening on %s", address)
	stdlog.Fatal(server.ListenAndServe())
}

func newRPCGateway(client worldv1.WorldServiceClient, healthClients ...grpc_health_v1.HealthClient) http.Handler {
	return newRPCGatewayWithLimiter(client, data.NewMemoryRateLimiter(), healthClients...)
}

func newRPCGatewayWithLimiter(client worldv1.WorldServiceClient, limiter biz.RateLimiter, healthClients ...grpc_health_v1.HealthClient) http.Handler {
	result := &gateway{rpc: client, limits: limiter}
	if len(healthClients) > 0 {
		result.health = healthClients[0]
	}
	return result
}

func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
		if g.health != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			response, err := g.health.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
			if err != nil || response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
				writeRPCError(w, nil, -32002, "game authority unavailable", http.StatusServiceUnavailable)
				return
			}
		}
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
			"instructions":    "Call get_game_rules before the first state-changing action and whenever the observed rule version changes. Then call get_state and use its latest life_number and state_version.",
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusNoContent)
	case "tools/list":
		writeRPCResult(w, request.ID, map[string]any{"tools": tools()})
	case "tools/call":
		allowed, err := g.limits.Allow(r.Context(), "mcp_tool", authorization, biz.MCPToolRateLimit)
		if err != nil {
			writeRPCError(w, request.ID, -32004, "role call budget unavailable", http.StatusServiceUnavailable)
			return
		}
		if !allowed {
			writeRPCError(w, request.ID, -32003, "role call budget exceeded", http.StatusTooManyRequests)
			return
		}
		g.callToolRPC(r.Context(), w, request, authorization)
	default:
		writeRPCError(w, request.ID, -32601, "method not found", http.StatusOK)
	}
}

func (g *gateway) callToolRPC(ctx context.Context, w http.ResponseWriter, request rpcRequest, authorization string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", authorization)
	key, _ := request.Params.Arguments["idempotency_key"].(string)
	if key == "" {
		key = "mcp-" + strings.Trim(string(request.ID), `"`)
	}
	var expectedLifeNumber, expectedStateVersion int64
	if toolRequiresExpectation(request.Params.Name) {
		var lifeOK, versionOK bool
		expectedLifeNumber, lifeOK = integerArgument(request.Params.Arguments, "expected_life_number")
		expectedStateVersion, versionOK = integerArgument(request.Params.Arguments, "expected_state_version")
		if !lifeOK || !versionOK || expectedLifeNumber <= 0 || expectedStateVersion <= 0 {
			writeRPCError(w, request.ID, -32602, "expected_life_number and expected_state_version are required", http.StatusOK)
			return
		}
	}
	var result proto.Message
	var err error
	switch request.Params.Name {
	case "get_game_rules":
		result, err = g.rpc.GetGameRules(ctx, &worldv1.GetGameRulesRequest{})
	case "get_state":
		result, err = g.rpc.GetState(ctx, &worldv1.GetStateRequest{})
	case "get_world_bounds":
		result, err = g.rpc.GetWorldBounds(ctx, &worldv1.GetWorldBoundsRequest{})
	case "scan":
		result, err = g.rpc.Scan(ctx, &worldv1.ScanRequest{ExpectedLifeNumber: expectedLifeNumber, ExpectedStateVersion: expectedStateVersion})
	case "move":
		x, xOK := numberArgument(request.Params.Arguments, "x")
		y, yOK := numberArgument(request.Params.Arguments, "y")
		if !xOK || !yOK {
			writeRPCError(w, request.ID, -32602, "x and y are required", http.StatusOK)
			return
		}
		result, err = g.rpc.Move(ctx, &worldv1.MoveRequest{IdempotencyKey: key, Target: &worldv1.Position{XMilliunits: int64(math.Round(x * 1000)), YMilliunits: int64(math.Round(y * 1000))}, ExpectedLifeNumber: expectedLifeNumber, ExpectedStateVersion: expectedStateVersion})
	case "move_direction":
		speed, ok := integerArgument(request.Params.Arguments, "speed")
		if !ok {
			writeRPCError(w, request.ID, -32602, "speed is required", http.StatusOK)
			return
		}
		result, err = g.rpc.MoveDirection(ctx, &worldv1.MoveDirectionRequest{IdempotencyKey: key, Direction: directionArgument(request.Params.Arguments, "direction"), Speed: speed, ExpectedLifeNumber: expectedLifeNumber, ExpectedStateVersion: expectedStateVersion})
	case "stop":
		result, err = g.rpc.Stop(ctx, &worldv1.StopRequest{IdempotencyKey: key, ExpectedLifeNumber: expectedLifeNumber, ExpectedStateVersion: expectedStateVersion})
	case "recent_events":
		result, err = g.rpc.ListRecentEvents(ctx, &worldv1.ListRecentEventsRequest{Limit: 50})
	case "list_conversations":
		result, err = g.rpc.ListConversations(ctx, &worldv1.ListConversationsRequest{})
	case "request_conversation":
		result, err = g.rpc.RequestConversation(ctx, &worldv1.RequestConversationRequest{IdempotencyKey: key, TargetId: stringArgument(request.Params.Arguments, "target_id"), ExpectedLifeNumber: expectedLifeNumber, ExpectedStateVersion: expectedStateVersion})
	case "respond_conversation":
		result, err = g.rpc.RespondConversation(ctx, &worldv1.RespondConversationRequest{IdempotencyKey: key, ConversationId: stringArgument(request.Params.Arguments, "conversation_id"), Action: stringArgument(request.Params.Arguments, "action"), ExpectedLifeNumber: expectedLifeNumber, ExpectedStateVersion: expectedStateVersion})
	case "send_conversation_message":
		result, err = g.rpc.SendConversationMessage(ctx, &worldv1.SendConversationMessageRequest{IdempotencyKey: key, ConversationId: stringArgument(request.Params.Arguments, "conversation_id"), Content: stringArgument(request.Params.Arguments, "content"), ExpectedLifeNumber: expectedLifeNumber, ExpectedStateVersion: expectedStateVersion})
	case "close_conversation":
		result, err = g.rpc.CloseConversation(ctx, &worldv1.CloseConversationRequest{IdempotencyKey: key, ConversationId: stringArgument(request.Params.Arguments, "conversation_id"), ExpectedLifeNumber: expectedLifeNumber, ExpectedStateVersion: expectedStateVersion})
	case "transfer_cultivation":
		amount, ok := integerArgument(request.Params.Arguments, "amount_minutes")
		if !ok {
			writeRPCError(w, request.ID, -32602, "amount_minutes is required", http.StatusOK)
			return
		}
		result, err = g.rpc.TransferCultivation(ctx, &worldv1.TransferCultivationRequest{IdempotencyKey: key, TargetId: stringArgument(request.Params.Arguments, "target_id"), AmountMinutes: amount, ExpectedLifeNumber: expectedLifeNumber, ExpectedStateVersion: expectedStateVersion})
	case "seize_cultivation":
		result, err = g.rpc.SeizeCultivation(ctx, &worldv1.SeizeCultivationRequest{IdempotencyKey: key, TargetId: stringArgument(request.Params.Arguments, "target_id"), ExpectedLifeNumber: expectedLifeNumber, ExpectedStateVersion: expectedStateVersion})
	case "reincarnate":
		random, _ := request.Params.Arguments["random"].(bool)
		reincarnate := &worldv1.ReincarnateRequest{IdempotencyKey: key, Random: random, ExpectedLifeNumber: expectedLifeNumber, ExpectedStateVersion: expectedStateVersion}
		if !random {
			x, xOK := numberArgument(request.Params.Arguments, "x")
			y, yOK := numberArgument(request.Params.Arguments, "y")
			if !xOK || !yOK {
				writeRPCError(w, request.ID, -32602, "x and y are required unless random is true", http.StatusOK)
				return
			}
			reincarnate.Position = &worldv1.Position{XMilliunits: int64(math.Round(x * 1000)), YMilliunits: int64(math.Round(y * 1000))}
		}
		result, err = g.rpc.Reincarnate(ctx, reincarnate)
	default:
		writeRPCError(w, request.ID, -32602, "unknown tool", http.StatusOK)
		return
	}
	if err != nil {
		writeRPCResult(w, request.ID, map[string]any{"content": []map[string]string{{"type": "text", "text": err.Error()}}, "isError": true})
		return
	}
	payload, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(result)
	if err != nil {
		writeRPCError(w, request.ID, -32603, "encode authority response", http.StatusOK)
		return
	}
	writeRPCResult(w, request.ID, map[string]any{"content": []map[string]string{{"type": "text", "text": string(payload)}}, "isError": false})
}

func toolRequiresExpectation(name string) bool {
	switch name {
	case "scan", "move", "move_direction", "stop", "request_conversation", "respond_conversation", "send_conversation_message", "close_conversation", "transfer_cultivation", "seize_cultivation", "reincarnate":
		return true
	default:
		return false
	}
}

func stringArgument(arguments map[string]any, key string) string {
	value, _ := arguments[key].(string)
	return value
}

func directionArgument(arguments map[string]any, key string) worldv1.Direction {
	switch stringArgument(arguments, key) {
	case "up":
		return worldv1.Direction_DIRECTION_UP
	case "down":
		return worldv1.Direction_DIRECTION_DOWN
	case "left":
		return worldv1.Direction_DIRECTION_LEFT
	case "right":
		return worldv1.Direction_DIRECTION_RIGHT
	default:
		return worldv1.Direction_DIRECTION_UNSPECIFIED
	}
}

func numberArgument(arguments map[string]any, key string) (float64, bool) {
	value, ok := arguments[key].(float64)
	return value, ok
}

func integerArgument(arguments map[string]any, key string) (int64, bool) {
	value, ok := numberArgument(arguments, key)
	if !ok || value != math.Trunc(value) {
		return 0, false
	}
	return int64(value), true
}

func tools() []tool {
	object := func(properties map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	}
	stringField := map[string]any{"type": "string"}
	numberField := map[string]any{"type": "number"}
	integerField := map[string]any{"type": "integer", "minimum": 1}
	keyField := map[string]any{"type": "string", "description": "Retry-stable idempotency key"}
	expectedLifeField := map[string]any{"type": "integer", "minimum": 1, "description": "Life number observed before issuing the command"}
	expectedVersionField := map[string]any{"type": "integer", "minimum": 1, "description": "State version observed before issuing the command"}
	return []tool{
		{Name: "get_game_rules", Description: "Read the current versioned authoritative game rules for this human-and-AI controlled role", InputSchema: object(nil)},
		{Name: "get_state", Description: "Read the authoritative current role state", InputSchema: object(nil)},
		{Name: "get_world_bounds", Description: "Read the explored world bounds", InputSchema: object(nil)},
		{Name: "scan", Description: "Perform one 神识扫描 snapshot with authoritative transfer, seizure, and conversation eligibility; each role may scan at most once per second across Web and MCP", InputSchema: object(map[string]any{"expected_life_number": expectedLifeField, "expected_state_version": expectedVersionField}, "expected_life_number", "expected_state_version")},
		{Name: "move", Description: "Start or replace continuous movement", InputSchema: object(map[string]any{"x": numberField, "y": numberField, "idempotency_key": keyField, "expected_life_number": expectedLifeField, "expected_state_version": expectedVersionField}, "x", "y", "expected_life_number", "expected_state_version")},
		{Name: "move_direction", Description: "Move continuously up, down, left, or right at a chosen speed no greater than the current realm speed", InputSchema: object(map[string]any{"direction": map[string]any{"type": "string", "enum": []string{"up", "down", "left", "right"}}, "speed": integerField, "idempotency_key": keyField, "expected_life_number": expectedLifeField, "expected_state_version": expectedVersionField}, "direction", "speed", "expected_life_number", "expected_state_version")},
		{Name: "stop", Description: "Stop at the authoritative current position", InputSchema: object(map[string]any{"idempotency_key": keyField, "expected_life_number": expectedLifeField, "expected_state_version": expectedVersionField}, "expected_life_number", "expected_state_version")},
		{Name: "recent_events", Description: "Read durable recent events", InputSchema: object(nil)},
		{Name: "list_conversations", Description: "List conversations and untrusted role messages", InputSchema: object(nil)},
		{Name: "request_conversation", Description: "Request conversation with a sensed role", InputSchema: object(map[string]any{"target_id": stringField, "idempotency_key": keyField, "expected_life_number": expectedLifeField, "expected_state_version": expectedVersionField}, "target_id", "expected_life_number", "expected_state_version")},
		{Name: "respond_conversation", Description: "Accept, reject, or ignore an incoming conversation", InputSchema: object(map[string]any{"conversation_id": stringField, "action": map[string]any{"type": "string", "enum": []string{"accept", "reject", "ignore"}}, "idempotency_key": keyField, "expected_life_number": expectedLifeField, "expected_state_version": expectedVersionField}, "conversation_id", "action", "expected_life_number", "expected_state_version")},
		{Name: "send_conversation_message", Description: "Send untrusted role content in an accepted conversation", InputSchema: object(map[string]any{"conversation_id": stringField, "content": stringField, "idempotency_key": keyField, "expected_life_number": expectedLifeField, "expected_state_version": expectedVersionField}, "conversation_id", "content", "expected_life_number", "expected_state_version")},
		{Name: "close_conversation", Description: "Close a conversation", InputSchema: object(map[string]any{"conversation_id": stringField, "idempotency_key": keyField, "expected_life_number": expectedLifeField, "expected_state_version": expectedVersionField}, "conversation_id", "expected_life_number", "expected_state_version")},
		{Name: "transfer_cultivation", Description: "Transfer positive whole cultivation minutes", InputSchema: object(map[string]any{"target_id": stringField, "amount_minutes": integerField, "idempotency_key": keyField, "expected_life_number": expectedLifeField, "expected_state_version": expectedVersionField}, "target_id", "amount_minutes", "expected_life_number", "expected_state_version")},
		{Name: "seize_cultivation", Description: "夺功 from a strictly lower-realm role within an inclusive authoritative distance of 1 world unit", InputSchema: object(map[string]any{"target_id": stringField, "idempotency_key": keyField, "expected_life_number": expectedLifeField, "expected_state_version": expectedVersionField}, "target_id", "expected_life_number", "expected_state_version")},
		{Name: "reincarnate", Description: "Reincarnate at an in-bounds coordinate or randomly", InputSchema: object(map[string]any{"x": numberField, "y": numberField, "random": map[string]any{"type": "boolean"}, "idempotency_key": keyField, "expected_life_number": expectedLifeField, "expected_state_version": expectedVersionField}, "expected_life_number", "expected_state_version")},
	}
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
