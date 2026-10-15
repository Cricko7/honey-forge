package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"honey-forge/internal/configschema"
	"honey-forge/internal/contract"
	"honey-forge/internal/mutation"
	"honey-forge/internal/postgres"
	"honey-forge/internal/support"
	"honey-forge/internal/traplinks"
	"honey-forge/modules/agentws"
	"honey-forge/modules/audit"
	authhttp "honey-forge/modules/auth/http"
	authrepo "honey-forge/modules/auth/repository"
	authservice "honey-forge/modules/auth/service"
	"honey-forge/modules/catalog"
	commandhttp "honey-forge/modules/commands/http"
	commandrepo "honey-forge/modules/commands/repository"
	commandservice "honey-forge/modules/commands/service"
	"honey-forge/modules/events"
	"honey-forge/modules/frontendws"
	"honey-forge/modules/profiles"
	profilerepo "honey-forge/modules/profiles/repository"
	profileservice "honey-forge/modules/profiles/service"
	"honey-forge/modules/traps"
)

type Config struct {
	SupportSender      support.Sender
	AgentClientCAFile  string
	DatabaseURL        string
	CursorKey          []byte
	BrowserOrigins     []string
	MigrationsPath     string
	Logger             *slog.Logger
	AgentGateway       agentws.Gateway
	AgentCommands      agentws.CommandService
	AgentWSURL         string
	PublicOrigin       string
	AutoDeployTraps    bool
	APIKeyRateLimit    int
	HistoryRetention   time.Duration
	Ingester           traps.Ingester
	EventPublisher     events.BatchPublisher
	CatalogDefinitions []catalog.Definition
	// Только доверенные прокси могут задавать ClientIP через X-Forwarded-For.
	TrustedProxies []string
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
	Frontend  *frontendws.Handler
	stop      context.CancelFunc
	done      chan struct{}
}

func Open(ctx context.Context, config Config) (*Runtime, error) {
	if config.HistoryRetention < 0 {
		return nil, fmt.Errorf("history retention must be positive")
	}
	endpoint, err := configureAgentEndpoint(config.AgentWSURL, config.PublicOrigin, config.BrowserOrigins)
	if err != nil {
		return nil, err
	}

	cursors, err := contract.NewCursorCodec(config.CursorKey)
	if err != nil {
		return nil, fmt.Errorf("configure cursor: %w", err)
	}

	browser, err := contract.NewBrowserPolicy(config.BrowserOrigins)
	if err != nil {
		return nil, fmt.Errorf("configure browser policy: %w", err)
	}

	pool, err := postgres.Open(ctx, config.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("initialize database: %w", err)
	}

	// Transfer ownership only after the complete application is wired.
	initialized := false
	defer func() {
		if !initialized {
			pool.Close()
		}
	}()

	directory := config.MigrationsPath
	if directory == "" {
		directory = "../../migrations"
	}

	if err := postgres.Migrate(ctx, pool, os.DirFS(directory)); err != nil {
		return nil, err
	}

	repository := catalog.NewRepository(pool)
	definitions := config.CatalogDefinitions
	if definitions == nil {
		definitions, err = repository.BuiltinDefinitions(ctx)
		if err != nil {
			return nil, fmt.Errorf("load catalog: %w", err)
		}
	}

	catalogService, err := catalog.NewService(definitions, cursors)
	if err != nil {
		return nil, fmt.Errorf("initialize catalog: %w", err)
	}

	if err := repository.Install(ctx, catalogService); err != nil {
		return nil, fmt.Errorf("install catalog: %w", err)
	}

	router := NewRouter(telemetryHost(endpoint), browser.CORSMiddleware())
	if len(config.TrustedProxies) > 0 {
		if err := router.SetTrustedProxies(config.TrustedProxies); err != nil {
			return nil, fmt.Errorf("configure trusted proxies: %w", err)
		}
	}

	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}

	profileService := profileservice.NewService(profilerepo.NewRepository(pool), profiles.Dependencies{LookupType: ProfileTypeLookup(catalogService)})
	authService := authservice.NewService(authrepo.NewRepository(pool)).WithAPIKeyRateLimit(config.APIKeyRateLimit)
	if err := RegisterServices(router, browser, authService, profileService, catalogService, logger); err != nil {
		return nil, err
	}

	trapRepository := traps.NewRepository(pool)
	eventRepository := events.NewRepository(pool, trapRepository, catalogService)
	commandRepository := commandrepo.New(pool, trapRepository)
	ingester := config.Ingester
	if ingester == nil {
		ingester = events.NewService(eventRepository, catalogService)
		if config.EventPublisher != nil {
			ingester = events.NewJournalService(eventRepository, catalogService, config.EventPublisher)
		}
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
	if config.AgentGateway == nil {
		agentHandler.PipelineStore = trapRepository
	}
	if pipelineGateway, ok := gateway.(interface {
		PipelineSnapshot(context.Context, agentws.Identity, string) (profiles.Snapshot, error)
	}); ok {
		agentHandler.PipelineSnapshot = pipelineGateway.PipelineSnapshot
	}
	links := traplinks.NewService(traplinks.NewRepository(pool))
	agentHandler.TrafficTargets = links.Targets
	agentHandler.ClientCAFile = config.AgentClientCAFile
	trapService := traps.NewService(trapRepository, catalogService, config.AgentWSURL, agentHandler.Revoke).WithAutoDeploy(config.AutoDeployTraps)
	session := authhttp.NewHandler(authService, logger).RequireSession()
	support.NewHandler(support.NewService(config.SupportSender), logger).Register(router, session)
	traplinks.NewHandler(links, logger).Register(router, session)
	audit.NewHandler(audit.NewService(audit.NewRepository(pool)), cursors, logger).RegisterRoutes(router, session)
	traps.NewHandler(trapService, cursors, logger).RegisterRoutes(router, session)
	events.NewHandler(eventRepository, cursors, logger).RegisterRoutes(router, session)
	commandhttp.NewHandler(commandservice.New(commandRepository, CommandActionCheck(catalogService)), cursors, logger).RegisterRoutes(router, session)
	agentHandler.Register(router)
	background, stop := context.WithCancel(context.WithoutCancel(ctx))
	frontend := frontendws.NewHandler(frontendws.NewRepository(pool), authService, cursors, browser, logger, background)
	frontend.Register(router)
	done := make(chan struct{})
	retentionDone := make(chan struct{})
	go func() {
		defer close(retentionDone)
		maintainHistory(background, pool, logger, config.HistoryRetention)
	}()
	go func() {
		defer close(done)
		defer func() { <-retentionDone }()

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
	initialized = true

	return &Runtime{
		Router:    router,
		Mutations: mutation.NewStore(pool),
		Schemas:   configschema.NewSchemaStore(pool),
		Cursors:   cursors,
		Browser:   browser,
		Catalog:   catalogService,
		pool:      pool,
		Traps:     trapService,
		Agents:    agents,
		Frontend:  frontend,
		stop:      stop,
		done:      done,
	}, nil
}

func (r *Runtime) Close() {
	r.stop()
	r.Frontend.Wait()
	<-r.done
	r.pool.Close()
}
