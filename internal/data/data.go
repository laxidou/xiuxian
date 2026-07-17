package data

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"

	"xiuxian/internal/biz"
	"xiuxian/internal/conf"
)

var ProviderSet = wire.NewSet(
	conf.ProvideData,
	NewData,
	NewWorldRepository,
	NewRateLimiter,
	NewDependencyHealthChecker,
)

type Data struct {
	postgres *PostgresSnapshotStore
	redis    *redis.Client
	log      *log.Helper
}

func NewData(config *conf.Data, logger log.Logger) (*Data, func(), error) {
	data := &Data{log: log.NewHelper(logger)}
	cleanup := func() {
		if data.redis != nil {
			if err := data.redis.Close(); err != nil {
				data.log.Warnw("operation", "close_redis", "error", err)
			}
		}
		if data.postgres != nil {
			if err := data.postgres.Close(); err != nil {
				data.log.Warnw("operation", "close_postgres", "error", err)
			}
		}
	}
	if config.RedisURL != "" {
		options, err := redis.ParseURL(config.RedisURL)
		if err != nil {
			return nil, cleanup, err
		}
		data.redis = redis.NewClient(options)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := data.redis.Ping(ctx).Err(); err != nil {
			return nil, cleanup, err
		}
	}
	if config.DatabaseURL == "" {
		data.log.Info("world repository uses in-memory durability")
		return data, cleanup, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := OpenPostgres(ctx, config.DatabaseURL)
	if err != nil {
		return nil, cleanup, err
	}
	data.postgres = store
	return data, cleanup, nil
}

type worldRepository struct {
	data *Data
}

type dependencyHealthChecker struct {
	data *Data
}

func NewDependencyHealthChecker(data *Data) biz.DependencyHealthChecker {
	return &dependencyHealthChecker{data: data}
}

func (checker *dependencyHealthChecker) Health(ctx context.Context) biz.DependencyHealth {
	health := biz.DependencyHealth{Postgres: "disabled", Redis: "disabled"}
	if checker.data.postgres != nil {
		health.Postgres = "ok"
		if err := checker.data.postgres.sqlDB.PingContext(ctx); err != nil {
			health.Postgres = "unavailable"
		}
	}
	if checker.data.redis != nil {
		health.Redis = "ok"
		if err := checker.data.redis.Ping(ctx).Err(); err != nil {
			health.Redis = "unavailable"
		}
	}
	return health
}

func NewWorldRepository(data *Data) biz.WorldRepository {
	return &worldRepository{data: data}
}

func (repository *worldRepository) Load(ctx context.Context) ([]byte, error) {
	if repository.data.postgres == nil {
		return nil, nil
	}
	return repository.data.postgres.Load(ctx)
}

func (repository *worldRepository) Save(ctx context.Context, payload []byte) error {
	if repository.data.postgres == nil {
		return nil
	}
	return repository.data.postgres.Save(ctx, payload)
}

func (repository *worldRepository) AuthorityNow(ctx context.Context) (time.Time, error) {
	if repository.data.postgres == nil {
		return time.Now().UTC(), nil
	}
	return repository.data.postgres.AuthorityNow(ctx)
}
