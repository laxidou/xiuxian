package data

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"

	"xiuxian/internal/biz"
	"xiuxian/internal/conf"
	"xiuxian/internal/storage"
)

var ProviderSet = wire.NewSet(
	conf.ProvideData,
	NewData,
	NewWorldRepository,
)

type Data struct {
	postgres *storage.PostgresSnapshotStore
	log      *log.Helper
}

func NewData(config *conf.Data, logger log.Logger) (*Data, func(), error) {
	data := &Data{log: log.NewHelper(logger)}
	cleanup := func() {}
	if config.DatabaseURL == "" {
		data.log.Info("world repository uses in-memory durability")
		return data, cleanup, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := storage.OpenPostgres(ctx, config.DatabaseURL)
	if err != nil {
		return nil, cleanup, err
	}
	data.postgres = store
	cleanup = func() {
		if err := store.Close(); err != nil {
			data.log.Warnw("operation", "close_postgres", "error", err)
		}
	}
	return data, cleanup, nil
}

type worldRepository struct {
	data *Data
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
