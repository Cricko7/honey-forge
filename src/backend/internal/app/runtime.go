package app

import (
	"context"
	"fmt"
	"honey-forge/internal/configschema"
	"honey-forge/internal/contract"
	"honey-forge/internal/mutation"
	"honey-forge/internal/postgres"
	authrepo "honey-forge/modules/auth/repository"
	authservice "honey-forge/modules/auth/service"
	"honey-forge/modules/catalog"
	"honey-forge/modules/profiles"
	profilerepo "honey-forge/modules/profiles/repository"
	profileservice "honey-forge/modules/profiles/service"
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	DatabaseURL    string
	CursorKey      []byte
	BrowserOrigins []string
	MigrationsPath string
	Logger         *slog.Logger
}
type Runtime struct {
	Router    *gin.Engine
	Mutations *mutation.Store
	Schemas   *configschema.SchemaStore
	Cursors   *contract.CursorCodec
	Browser   *contract.BrowserPolicy
	Catalog   *catalog.Service
	pool      *pgxpool.Pool
}

func Open(ctx context.Context, config Config) (*Runtime, error) {
	cursors, err := contract.NewCursorCodec(config.CursorKey)
	if err != nil {
		return nil, fmt.Errorf("configure cursor: %w", err)
	}
	browser, err := contract.NewBrowserPolicy(config.BrowserOrigins)
	if err != nil {
		return nil, fmt.Errorf("configure browser policy: %w", err)
	}
	catalogService, err := catalog.NewService(catalog.BuiltinDefinitions(), cursors)
	if err != nil {
		return nil, fmt.Errorf("initialize catalog: %w", err)
	}
	pool, err := postgres.Open(ctx, config.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("initialize database: %w", err)
	}
	directory := config.MigrationsPath
	if directory == "" {
		directory = "../../migrations"
	}
	if err := postgres.Migrate(ctx, pool, os.DirFS(directory)); err != nil {
		pool.Close()
		return nil, err
	}
	if err := catalog.NewRepository(pool).Install(ctx, catalogService); err != nil {
		pool.Close()
		return nil, fmt.Errorf("install catalog: %w", err)
	}
	router := NewRouter()
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	profileService := profileservice.NewService(profilerepo.NewRepository(pool), profiles.Dependencies{LookupType: ProfileTypeLookup(catalogService), HasLiveBindings: profilerepo.CheckLiveBindings})
	if err := RegisterServices(router, browser, authservice.NewService(authrepo.NewRepository(pool)), profileService, catalogService, logger); err != nil {
		pool.Close()
		return nil, err
	}
	return &Runtime{Router: router, Mutations: mutation.NewStore(pool), Schemas: configschema.NewSchemaStore(pool), Cursors: cursors, Browser: browser, Catalog: catalogService, pool: pool}, nil
}
func (r *Runtime) Close() { r.pool.Close() }
