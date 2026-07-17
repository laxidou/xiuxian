package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"xiuxian/internal/biz"
	"xiuxian/internal/data"
	"xiuxian/internal/service"
)

type fixedHealthChecker struct {
	health biz.DependencyHealth
}

func (checker fixedHealthChecker) Health(context.Context) biz.DependencyHealth {
	return checker.health
}

func TestHealthDistinguishesAuthoritativeAndDegradedDependencies(t *testing.T) {
	tests := []struct {
		name       string
		health     biz.DependencyHealth
		wantStatus int
		wantState  string
	}{
		{name: "redis unavailable", health: biz.DependencyHealth{Postgres: "ok", Redis: "unavailable"}, wantStatus: http.StatusServiceUnavailable, wantState: "unavailable"},
		{name: "postgres unavailable", health: biz.DependencyHealth{Postgres: "unavailable", Redis: "ok"}, wantStatus: http.StatusServiceUnavailable, wantState: "unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := service.NewAuxiliaryHTTPHandler(nil, data.NewMemoryRateLimiter(), service.AuxiliaryHTTPOptions{Version: "test"}, fixedHealthChecker{health: test.health})
			request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			var body map[string]string
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["status"] != test.wantState || body["postgres"] != test.health.Postgres || body["redis"] != test.health.Redis {
				t.Fatalf("health = %#v", body)
			}
		})
	}
}
