package conf

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/config"
	"github.com/go-kratos/kratos/v2/config/file"
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

// Load builds configuration from a layered source precedence:
//   1. defaults (hard-coded)
//   2. configs/config.yaml (optional)
//   3. environment variables (override, backward-compatible names)
func Load() *Config {
	cfg := &Config{
		Server: Server{
			HTTPAddress:  ":8080",
			GRPCAddress:  ":9090",
			HTTPTimeout:  30 * time.Second,
			GRPCTimeout:  30 * time.Second,
			SecureCookie: true,
			Version:      "dev",
		},
	}

	// Layer 2: file configuration (optional)
	c := config.New(config.WithSource(file.NewSource("configs/config.yaml")))
	if err := c.Load(); err == nil {
		applyFileConfig(c, cfg)
		_ = c.Close()
	}

	// Layer 3: environment variable overrides (preserves original naming)
	overrideFromEnv(cfg)

	return cfg
}

func applyFileConfig(c config.Config, cfg *Config) {
	if v := c.Value("server.http_address"); v.Load() != nil {
		if s, err := v.String(); err == nil && s != "" {
			cfg.Server.HTTPAddress = s
		}
	}
	if v := c.Value("server.grpc_address"); v.Load() != nil {
		if s, err := v.String(); err == nil && s != "" {
			cfg.Server.GRPCAddress = s
		}
	}
	if v := c.Value("server.http_timeout"); v.Load() != nil {
		if s, err := v.String(); err == nil && s != "" {
			if d, err := time.ParseDuration(s); err == nil {
				cfg.Server.HTTPTimeout = d
			}
		}
	}
	if v := c.Value("server.grpc_timeout"); v.Load() != nil {
		if s, err := v.String(); err == nil && s != "" {
			if d, err := time.ParseDuration(s); err == nil {
				cfg.Server.GRPCTimeout = d
			}
		}
	}
	if v := c.Value("server.secure_cookie"); v.Load() != nil {
		if b, err := v.Bool(); err == nil {
			cfg.Server.SecureCookie = b
		}
	}
	if v := c.Value("server.worker_token"); v.Load() != nil {
		if s, err := v.String(); err == nil {
			cfg.Server.WorkerToken = s
		}
	}
	if v := c.Value("server.version"); v.Load() != nil {
		if s, err := v.String(); err == nil && s != "" {
			cfg.Server.Version = s
		}
	}
	if v := c.Value("server.allow_test_clock"); v.Load() != nil {
		if b, err := v.Bool(); err == nil {
			cfg.Server.AllowTestClock = b
		}
	}
	if v := c.Value("server.test_clock_start_millis"); v.Load() != nil {
		if i, err := v.Int(); err == nil {
			cfg.Server.TestClockStartMillis = i
		}
	}
	if v := c.Value("data.database_url"); v.Load() != nil {
		if s, err := v.String(); err == nil {
			cfg.Data.DatabaseURL = s
		}
	}
	if v := c.Value("data.redis_url"); v.Load() != nil {
		if s, err := v.String(); err == nil {
			cfg.Data.RedisURL = s
		}
	}
}

func overrideFromEnv(cfg *Config) {
	if v := envOrDefault("GAME_SERVER_ADDRESS", ""); v != "" {
		cfg.Server.HTTPAddress = v
	}
	if v := envOrDefault("GAME_SERVER_GRPC_ADDRESS", ""); v != "" {
		cfg.Server.GRPCAddress = v
	}
	if v := strings.TrimSpace(os.Getenv("APP_VERSION")); v != "" {
		cfg.Server.Version = v
	}
	if os.Getenv("COOKIE_SECURE") == "false" {
		cfg.Server.SecureCookie = false
	} else if os.Getenv("COOKIE_SECURE") == "true" {
		cfg.Server.SecureCookie = true
	}
	cfg.Server.WorkerToken = os.Getenv("WORKER_TOKEN")
	cfg.Server.AllowTestClock = os.Getenv("ALLOW_TEST_CLOCK") == "true"
	cfg.Server.TestClockStartMillis = envInt64("TEST_CLOCK_START_UNIX_MILLIS")
	cfg.Data.DatabaseURL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	cfg.Data.RedisURL = strings.TrimSpace(os.Getenv("REDIS_URL"))
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
