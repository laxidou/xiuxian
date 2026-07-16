package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMCPListsToolsAndCallsAuthoritativeGameAPI(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer xiu_test" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/api/v1/state" {
			t.Fatalf("path = %q, want state", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "青玄", "status": "alive"})
	}))
	defer upstream.Close()
	gateway := newGateway(upstream.URL, upstream.Client())

	listBody := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	listRequest := httptest.NewRequest(http.MethodPost, "/mcp", listBody)
	listRequest.Header.Set("Authorization", "Bearer xiu_test")
	listResponse := httptest.NewRecorder()
	gateway.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !bytes.Contains(listResponse.Body.Bytes(), []byte(`"get_state"`)) || !bytes.Contains(listResponse.Body.Bytes(), []byte(`"seize_cultivation"`)) {
		t.Fatalf("tools/list response = %d %s", listResponse.Code, listResponse.Body.String())
	}

	callBody := bytes.NewBufferString(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_state","arguments":{}}}`)
	callRequest := httptest.NewRequest(http.MethodPost, "/mcp", callBody)
	callRequest.Header.Set("Authorization", "Bearer xiu_test")
	callResponse := httptest.NewRecorder()
	gateway.ServeHTTP(callResponse, callRequest)
	if callResponse.Code != http.StatusOK || !bytes.Contains(callResponse.Body.Bytes(), []byte(`青玄`)) {
		t.Fatalf("tools/call response = %d %s", callResponse.Code, callResponse.Body.String())
	}
}
