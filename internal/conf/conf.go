package conf

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Server Server
	Data   Data
}

type Server struct {
	HTTPAddress          string
	GRPCAddress          string
	HTTPTimeout          time.Duration
	GRPCTimeout          time.Duration
	SecureCookie         bool
	WorkerToken          string
	Version              string
	AllowTestClock       bool
	TestClockStartMillis int64
}

type Data struct {
	DatabaseURL string
	RedisURL    string
}

func Load() *Config {
	httpAddress := envOrDefault("GAME_SERVER_ADDRESS", ":8080")
	grpcAddress := envOrDefault("GAME_SERVER_GRPC_ADDRESS", ":9090")
	version := strings.TrimSpace(os.Getenv("APP_VERSION"))
	if version == "" {
		version = "dev"
	}
	return &Config{
		Server: Server{
			HTTPAddress:          httpAddress,
			GRPCAddress:          grpcAddress,
			HTTPTimeout:          30 * time.Second,
			GRPCTimeout:          30 * time.Second,
			SecureCookie:         os.Getenv("COOKIE_SECURE") != "false",
			WorkerToken:          os.Getenv("WORKER_TOKEN"),
			Version:              version,
			AllowTestClock:       os.Getenv("ALLOW_TEST_CLOCK") == "true",
			TestClockStartMillis: envInt64("TEST_CLOCK_START_UNIX_MILLIS"),
		},
		Data: Data{
			DatabaseURL: strings.TrimSpace(os.Getenv("DATABASE_URL")),
			RedisURL:    strings.TrimSpace(os.Getenv("REDIS_URL")),
		},
	}
}

func envInt64(key string) int64 {
	value, _ := strconv.ParseInt(strings.TrimSpace(os.Getenv(key)), 10, 64)
	return value
}

func ProvideServer(config *Config) *Server { return &config.Server }

func ProvideData(config *Config) *Data { return &config.Data }

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
