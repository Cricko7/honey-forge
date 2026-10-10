package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"time"

	"honey-forge/internal/configschema"
	"honey-forge/internal/contract"
	"honey-forge/internal/mutation"
	"honey-forge/internal/postgres"
	"honey-forge/modules/agentws"
	authhttp "honey-forge/modules/auth/http"
	authrepo "honey-forge/modules/auth/repository"
	authservice "honey-forge/modules/auth/service"
	"honey-forge/modules/catalog"
	commandhttp "honey-forge/modules/commands/http"
	commandrepo "honey-forge/modules/commands/repository"
	commandservice "honey-forge/modules/commands/service"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
	profilerepo "honey-forge/modules/profiles/repository"
	profileservice "honey-forge/modules/profiles/service"
	"honey-forge/modules/traps"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	DatabaseURL    string
	CursorKey      []byte
	BrowserOrigins []string
	MigrationsPath string
	Logger         *slog.Logger
	AgentGateway   agentws.Gateway
	AgentCommands  agentws.CommandService
	AgentWSURL     string
	Ingester       traps.Ingester
}
type Runtime struct {
	Router    *gin.Engine
	Mutations *mutation.Store
	Schemas   *configschema.SchemaStore
	Cursors   *contract.CursorCodec
	Browser   *contract.BrowserPolicy
	Catalog   *catalog.Service
	pool      *pgxpool.Pool
	Traps     *traps.Service
	Agents    *traps.Gateway
	stop      context.CancelFunc
	done      chan struct{}
}

func Open(ctx context.Context, config Config) (*Runtime, error) {
	if config.AgentWSURL == "" {
		if len(config.BrowserOrigins) > 0 {
			origin, err := url.Parse(config.BrowserOrigins[0])
			if err == nil {
				config.AgentWSURL = "wss://" + origin.Host + "/assets/stream"
			}
		}
	}
	if endpoint, err := url.Parse(config.AgentWSURL); err != nil || endpoint.Scheme != "wss" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "/assets/stream" {
		return nil, fmt.Errorf("AgentWSURL must be a wss URL ending in /assets/stream")
	}
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
	authService := authservice.NewService(authrepo.NewRepository(pool))
	if err := RegisterServices(router, browser, authService, profileService, catalogService, logger); err != nil {
		pool.Close()
		return nil, err
	}
	trapRepository := traps.NewRepository(pool)
	eventRepository := events.NewRepository(pool, trapRepository)
	commandRepository := commandrepo.New(pool, trapRepository)
	ingester := config.Ingester
	if ingester == nil {
		ingester = events.NewService(eventRepository, catalogService)
	}
	agents := traps.NewGateway(trapRepository, catalogService, ingester)
	gateway := config.AgentGateway
	if gateway == nil {
		gateway = agents
	}
	agentCommands := config.AgentCommands
	if agentCommands == nil {
		agentCommands = commandservice.NewAgent(commandRepository, CommandResultCheck(catalogService))
	}
	agentHandler := agentws.NewHandler(gateway, agentCommands)
	trapService := traps.NewService(trapRepository, catalogService, config.AgentWSURL, agentHandler.Revoke)
	session := authhttp.NewHandler(authService, logger).RequireSession()
	traps.NewHandler(trapService, cursors, logger).RegisterRoutes(router, session)
	events.NewHandler(eventRepository, cursors, logger).RegisterRoutes(router, session)
	commandhttp.NewHandler(commandservice.New(commandRepository, CommandActionCheck(catalogService)), cursors, logger).RegisterRoutes(router, session)
	agentHandler.Register(router)
	background, stop := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-background.Done():
				return
			case now := <-ticker.C:
				work, cancel := context.WithTimeout(background, 5*time.Second)
				if err := agents.ExpireConnections(work, now); err != nil && background.Err() == nil {
					logger.ErrorContext(work, "expire trap connections failed", "error_type", fmt.Sprintf("%T", err))
				}
				if err := commandRepository.ExpireAllDue(work, now); err != nil && background.Err() == nil {
					logger.ErrorContext(work, "expire trap commands failed", "error_type", fmt.Sprintf("%T", err))
				}
				cancel()
			}
		}
	}()
	return &Runtime{Router: router, Mutations: mutation.NewStore(pool), Schemas: configschema.NewSchemaStore(pool), Cursors: cursors, Browser: browser, Catalog: catalogService, pool: pool, Traps: trapService, Agents: agents, stop: stop, done: done}, nil
}
func (r *Runtime) Close() { r.stop(); <-r.done; r.pool.Close() }
