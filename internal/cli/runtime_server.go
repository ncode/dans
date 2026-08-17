package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/ncode/dans/internal/database"
	"github.com/ncode/dans/internal/httpserver"
	"github.com/ncode/dans/internal/upstream"
)

// RuntimeServer assembles and owns one production API instance.
type RuntimeServer struct {
	logOutput io.Writer
}

// NewRuntimeServer creates a production server whose structured runtime and
// access logs are written to logOutput.
func NewRuntimeServer(logOutput io.Writer) *RuntimeServer {
	if logOutput == nil {
		logOutput = io.Discard
	}
	return &RuntimeServer{logOutput: logOutput}
}

func (runtime *RuntimeServer) Serve(ctx context.Context, config ServeConfig) error {
	if runtime == nil || ctx == nil {
		return errors.New("serve runtime: missing dependency")
	}
	configured := runtimeConfigurations(config)
	pools, err := database.OpenRuntimePools(ctx, configured.database)
	if err != nil {
		return fmt.Errorf("serve runtime: open PostgreSQL pools: %w", err)
	}
	defer pools.Close()

	store := database.NewStore(pools.Requests)
	authenticator := database.NewStore(pools.Authentication)
	readiness := database.NewStore(pools.Readiness)
	upstreamClient, err := upstream.New(configured.upstream, config.PowerDNSAPIKey)
	if err != nil {
		return fmt.Errorf("serve runtime: configure PowerDNS: %w", err)
	}
	auditHealth, err := httpserver.NewAuditHealth(readiness.ProbeAuditPersistence)
	if err != nil {
		return fmt.Errorf("serve runtime: configure audit health: %w", err)
	}
	workerCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()
	go runOverdueIntentCloser(workerCtx, store, auditHealth, time.Second)
	schemaProbe := func(ctx context.Context) error {
		return database.CheckRuntimeCompatibility(ctx, pools.Readiness)
	}
	powerDNSCompatibility := &httpserver.PowerDNSCompatibility{}
	powerDNSProbe := func(ctx context.Context) error {
		err := upstreamClient.Probe(ctx)
		powerDNSCompatibility.Record(err)
		return err
	}
	probeCtx, cancelProbe := context.WithTimeout(ctx, config.HealthTimeout)
	_ = powerDNSProbe(probeCtx)
	cancelProbe()
	health, err := httpserver.NewHealth(httpserver.Dependencies{
		Database: pools.Readiness.Ping,
		Schema:   schemaProbe,
		Audit:    auditHealth.Probe,
		PowerDNS: powerDNSProbe,
	}, config.HealthTimeout)
	if err != nil {
		return fmt.Errorf("serve runtime: configure health: %w", err)
	}
	dans, err := httpserver.NewDANSOperations(httpserver.GeneratedConfig{
		Store:           store,
		Upstream:        upstreamClient,
		UpstreamID:      config.PowerDNSUpstream,
		MutationTimeout: config.PowerDNSTimeout,
		AuditFailures:   auditHealth,
	})
	if err != nil {
		return fmt.Errorf("serve runtime: configure generated handlers: %w", err)
	}
	handler, err := httpserver.NewApplicationHandler(httpserver.ApplicationConfig{
		Boundary:      configured.boundary,
		Logger:        httpserver.NewJSONLogger(runtime.logOutput, slog.LevelInfo),
		Authenticator: authenticator,
		Schema: func(ctx context.Context) error {
			return database.CheckRuntimeCompatibility(ctx, pools.Requests)
		},
		PowerDNSReady:     powerDNSCompatibility.Compatible,
		Health:            health,
		Upstream:          upstreamClient,
		DANS:              dans,
		Mutations:         store,
		Lifecycle:         store,
		Denials:           store,
		AuditFailures:     auditHealth,
		LifecycleFailures: auditHealth,
		UpstreamID:        config.PowerDNSUpstream,
		MutationTimeout:   config.PowerDNSTimeout,
	})
	if err != nil {
		return fmt.Errorf("serve runtime: configure HTTP application: %w", err)
	}
	httpServer, err := httpserver.NewHTTPServer(configured.server, handler)
	if err != nil {
		return fmt.Errorf("serve runtime: configure HTTP server: %w", err)
	}
	return httpserver.ListenAndServe(ctx, httpServer, health, config.ShutdownTimeout)
}

type overdueIntentCloser interface {
	CloseOverdueDNSIntents(context.Context, int) (int, error)
}

type auditFailureReporter interface {
	ReportAuditFailure()
}

func runOverdueIntentCloser(ctx context.Context, closer overdueIntentCloser, failures auditFailureReporter, interval time.Duration) {
	if closer == nil || failures == nil || interval <= 0 || ctx.Err() != nil {
		return
	}
	closeOverdue := func() {
		if _, err := closer.CloseOverdueDNSIntents(ctx, 100); err != nil && ctx.Err() == nil {
			failures.ReportAuditFailure()
		}
	}
	closeOverdue()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			closeOverdue()
		}
	}
}

type runtimeConfigurationSet struct {
	database database.RuntimePoolConfig
	boundary httpserver.BoundaryConfig
	server   httpserver.ServerConfig
	upstream upstream.TransportConfig
}

func runtimeConfigurations(config ServeConfig) runtimeConfigurationSet {
	return runtimeConfigurationSet{
		database: database.RuntimePoolConfig{
			ConnectionString:      config.DatabaseURL.Value(),
			MaxConnections:        config.DatabaseMaxConnections,
			AuthenticationReserve: config.DatabaseAuthenticationReserve,
			ReadinessReserve:      config.DatabaseReadinessReserve,
			ConnectTimeout:        config.DatabaseConnectTimeout,
			StatementTimeout:      config.DatabaseStatementTimeout,
			LockTimeout:           config.DatabaseLockTimeout,
			TransactionTimeout:    config.DatabaseTransactionTimeout,
		},
		boundary: httpserver.BoundaryConfig{
			MaxBodyBytes:   config.MaxBodyBytes,
			RequestTimeout: config.RequestTimeout,
			MaxConcurrent:  config.MaxConcurrentRequests,
		},
		server: httpserver.ServerConfig{
			Address:           config.Address,
			MaxHeaderBytes:    config.MaxHeaderBytes,
			ReadHeaderTimeout: config.ReadHeaderTimeout,
			ReadTimeout:       config.ReadTimeout,
			WriteTimeout:      config.WriteTimeout,
			IdleTimeout:       config.IdleTimeout,
		},
		upstream: upstream.TransportConfig{
			URL:        config.PowerDNSURL,
			UnixSocket: config.PowerDNSUnixSocket,
			Timeout:    config.PowerDNSTimeout,
		},
	}
}

var _ Server = (*RuntimeServer)(nil)
