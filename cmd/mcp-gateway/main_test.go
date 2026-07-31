package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"

	worldv1 "xiuxian/gen/go/xiuxian/v1"
	"xiuxian/internal/biz"
	"xiuxian/internal/data"
	worldservice "xiuxian/internal/service"
)

func TestMCPListsToolsAndCallsAuthoritativeGameAPI(t *testing.T) {
	gateway := newRPCGateway(nil)
	initializeBody := bytes.NewBufferString(`{"jsonrpc":"2.0","id":0,"method":"initialize"}`)
	initializeRequest := httptest.NewRequest(http.MethodPost, "/mcp", initializeBody)
	initializeRequest.Header.Set("Authorization", "Bearer xiu_test")
	initializeResponse := httptest.NewRecorder()
	gateway.ServeHTTP(initializeResponse, initializeRequest)
	if initializeResponse.Code != http.StatusOK || !bytes.Contains(initializeResponse.Body.Bytes(), []byte(`get_game_rules`)) {
		t.Fatalf("initialize response = %d %s", initializeResponse.Code, initializeResponse.Body.String())
	}

	listBody := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	listRequest := httptest.NewRequest(http.MethodPost, "/mcp", listBody)
	listRequest.Header.Set("Authorization", "Bearer xiu_test")
	listResponse := httptest.NewRecorder()
	gateway.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !bytes.Contains(listResponse.Body.Bytes(), []byte(`"get_game_rules"`)) || !bytes.Contains(listResponse.Body.Bytes(), []byte(`"get_state"`)) || !bytes.Contains(listResponse.Body.Bytes(), []byte(`"seize_cultivation"`)) || bytes.Contains(listResponse.Body.Bytes(), []byte(`"reincarnate"`)) || !bytes.Contains(listResponse.Body.Bytes(), []byte(`once per second`)) || !bytes.Contains(listResponse.Body.Bytes(), []byte(`distance of 1 world unit`)) {
		t.Fatalf("tools/list response = %d %s", listResponse.Code, listResponse.Body.String())
	}

}

func TestMCPProductionGatewayUsesGeneratedGRPCContract(t *testing.T) {
	service := biz.NewService(biz.NewManualClock(time.UnixMilli(1_700_000_000_000)))
	_, state, err := service.Register(context.Background(), "grpc-owner", "a sufficiently long password", "契约真人")
	if err != nil {
		t.Fatal(err)
	}
	key, err := service.RotateMCPKey(context.Background(), state.ID)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	worldv1.RegisterWorldServiceServer(server, worldservice.NewWorldService(biz.NewWorldUsecase(service, log.DefaultLogger), data.NewMemoryRateLimiter()))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.NewClient("passthrough:///bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	gateway := newRPCGateway(worldv1.NewWorldServiceClient(connection))

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_game_rules","arguments":{}}}`)
	request := httptest.NewRequest(http.MethodPost, "/mcp", body)
	request.Header.Set("Authorization", "Bearer "+key)
	response := httptest.NewRecorder()
	gateway.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`rule_version\":4`)) || !bytes.Contains(response.Body.Bytes(), []byte(`get_state`)) {
		t.Fatalf("game rules MCP response = %d %s", response.Code, response.Body.String())
	}

	body = bytes.NewBufferString(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_state","arguments":{}}}`)
	request = httptest.NewRequest(http.MethodPost, "/mcp", body)
	request.Header.Set("Authorization", "Bearer "+key)
	response = httptest.NewRecorder()
	gateway.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`契约真人`)) || !bytes.Contains(response.Body.Bytes(), []byte(`realm_name`)) {
		t.Fatalf("gRPC MCP response = %d %s", response.Code, response.Body.String())
	}
	body = bytes.NewBufferString(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"move_direction","arguments":{"direction":"left","speed":1,"expected_life_number":1,"expected_state_version":2}}}`)
	request = httptest.NewRequest(http.MethodPost, "/mcp", body)
	request.Header.Set("Authorization", "Bearer "+key)
	response = httptest.NewRecorder()
	gateway.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`movement_direction\":\"left`)) {
		t.Fatalf("direction MCP response = %d %s", response.Code, response.Body.String())
	}
	for id := 5; id <= 6; id++ {
		body = bytes.NewBufferString(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"get_state","arguments":{}}}`, id))
		request = httptest.NewRequest(http.MethodPost, "/mcp", body)
		request.Header.Set("Authorization", "Bearer "+key)
		response = httptest.NewRecorder()
		gateway.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("burst call %d status = %d, want 200", id, response.Code)
		}
	}
	body = bytes.NewBufferString(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"get_state","arguments":{}}}`)
	request = httptest.NewRequest(http.MethodPost, "/mcp", body)
	request.Header.Set("Authorization", "Bearer "+key)
	response = httptest.NewRecorder()
	gateway.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth immediate call status = %d, want 429", response.Code)
	}
}

func TestMCPHealthReflectsGRPCAuthorityStatus(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.NewClient("passthrough:///bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	gateway := newRPCGateway(nil, grpc_health_v1.NewHealthClient(connection))

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	gateway.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("serving health status = %d, want 200", response.Code)
	}

	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	request = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response = httptest.NewRecorder()
	gateway.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("not-serving health status = %d, want 503", response.Code)
	}
}
