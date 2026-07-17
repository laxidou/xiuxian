package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/go-kratos/kratos/v2/log"

	"xiuxian/internal/biz"
	"xiuxian/internal/conf"
	"xiuxian/internal/server"
	"xiuxian/internal/service"
	"xiuxian/internal/world"
)

func TestKratosHTTPServerPreservesLegacyAndGeneratedRoutes(t *testing.T) {
	logger := log.NewStdLogger(io.Discard)
	usecase := biz.NewWorldUsecase(world.NewService(world.SystemClock{}), logger)
	config := &conf.Server{
		HTTPAddress:  ":0",
		HTTPTimeout:  0,
		SecureCookie: false,
		Version:      "test",
	}
	legacy := server.NewLegacyHTTPHandler(usecase, config)
	transport := server.NewHTTPServer(config, service.NewWorldService(usecase), legacy, logger)
	httpServer := httptest.NewServer(transport)
	defer httpServer.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	registerBody, _ := json.Marshal(map[string]string{
		"account": "kratos-http",
		"password": "a sufficiently long password",
		"role_name": "玄门行者",
	})
	response, err := client.Post(httpServer.URL+"/api/v1/auth/register", "application/json", bytes.NewReader(registerBody))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated {
		response.Body.Close()
		t.Fatalf("register status = %d", response.StatusCode)
	}
	var registered struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&registered); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()

	response, err = client.Post(httpServer.URL+"/xiuxian.v1.WorldService/GetState", "application/json", bytes.NewBufferString("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("generated state status = %d: %s", response.StatusCode, body)
	}
	var generated struct {
		ID         string `json:"id"`
		LifeNumber string `json:"lifeNumber"`
	}
	if err := json.NewDecoder(response.Body).Decode(&generated); err != nil {
		t.Fatal(err)
	}
	if generated.ID != registered.ID || generated.LifeNumber != "1" {
		t.Fatalf("generated state = %#v, registered id = %q", generated, registered.ID)
	}
}
