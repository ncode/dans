package database

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RuntimePoolConfig bounds all database connections owned by one API instance.
type RuntimePoolConfig struct {
	ConnectionString      string
	MaxConnections        int32
	AuthenticationReserve int32
	ReadinessReserve      int32
	ConnectTimeout        time.Duration
	StatementTimeout      time.Duration
	LockTimeout           time.Duration
	TransactionTimeout    time.Duration
}

// RuntimePoolConfigurationSet holds separate capacity for ordinary requests,
// authentication decisions, and readiness probes.
type RuntimePoolConfigurationSet struct {
	Requests       poolConfiguration
	Authentication poolConfiguration
	Readiness      poolConfiguration
}

type poolConfiguration struct {
	config *pgxpool.Config
}

// RuntimePools are three primary-only pgx pools whose combined maximum is the
// configured per-instance connection limit.
type RuntimePools struct {
	Requests       *pgxpool.Pool
	Authentication *pgxpool.Pool
	Readiness      *pgxpool.Pool
}

// RuntimePoolConfigs validates one immutable configuration and derives the
// three pool configurations without making a database connection.
func RuntimePoolConfigs(config RuntimePoolConfig) (RuntimePoolConfigurationSet, error) {
	if config.ConnectionString == "" {
		return RuntimePoolConfigurationSet{}, errors.New("database pool: connection string is required")
	}
	if config.AuthenticationReserve <= 0 || config.ReadinessReserve <= 0 || config.MaxConnections <= config.AuthenticationReserve+config.ReadinessReserve {
		return RuntimePoolConfigurationSet{}, errors.New("database pool: connection capacity must include request, authentication, and readiness connections")
	}
	if !boundedDuration(config.ConnectTimeout) || !boundedDuration(config.StatementTimeout) || !boundedDuration(config.LockTimeout) || !boundedDuration(config.TransactionTimeout) {
		return RuntimePoolConfigurationSet{}, errors.New("database pool: timeouts must be at least one millisecond")
	}

	base, err := pgxpool.ParseConfig(config.ConnectionString)
	if err != nil {
		return RuntimePoolConfigurationSet{}, errors.New("database pool: invalid connection configuration")
	}
	base.ConnConfig.ConnectTimeout = config.ConnectTimeout
	base.ConnConfig.Config.ValidateConnect = pgconn.ValidateConnectTargetSessionAttrsReadWrite
	if base.ConnConfig.Config.RuntimeParams == nil {
		base.ConnConfig.Config.RuntimeParams = make(map[string]string)
	}
	base.ConnConfig.Config.RuntimeParams["statement_timeout"] = milliseconds(config.StatementTimeout)
	base.ConnConfig.Config.RuntimeParams["lock_timeout"] = milliseconds(config.LockTimeout)
	base.ConnConfig.Config.RuntimeParams["idle_in_transaction_session_timeout"] = milliseconds(config.TransactionTimeout)
	base.MinConns = 0
	base.MinIdleConns = 0
	base.PingTimeout = config.ConnectTimeout

	requests := configuredPool(base, config.MaxConnections-config.AuthenticationReserve-config.ReadinessReserve, "dans-requests")
	authentication := configuredPool(base, config.AuthenticationReserve, "dans-authentication")
	readiness := configuredPool(base, config.ReadinessReserve, "dans-readiness")
	return RuntimePoolConfigurationSet{
		Requests:       poolConfiguration{config: requests},
		Authentication: poolConfiguration{config: authentication},
		Readiness:      poolConfiguration{config: readiness},
	}, nil
}

// OpenRuntimePools creates lazy pools; callers explicitly ping through the
// readiness pool before accepting authenticated traffic.
func OpenRuntimePools(ctx context.Context, config RuntimePoolConfig) (*RuntimePools, error) {
	configurations, err := RuntimePoolConfigs(config)
	if err != nil {
		return nil, err
	}
	requests, err := pgxpool.NewWithConfig(ctx, configurations.Requests.config)
	if err != nil {
		return nil, errors.New("database pool: create request pool")
	}
	authentication, err := pgxpool.NewWithConfig(ctx, configurations.Authentication.config)
	if err != nil {
		requests.Close()
		return nil, errors.New("database pool: create authentication pool")
	}
	readiness, err := pgxpool.NewWithConfig(ctx, configurations.Readiness.config)
	if err != nil {
		authentication.Close()
		requests.Close()
		return nil, errors.New("database pool: create readiness pool")
	}
	return &RuntimePools{Requests: requests, Authentication: authentication, Readiness: readiness}, nil
}

// Close releases every pool owned by the instance.
func (pools *RuntimePools) Close() {
	if pools == nil {
		return
	}
	if pools.Readiness != nil {
		pools.Readiness.Close()
	}
	if pools.Authentication != nil {
		pools.Authentication.Close()
	}
	if pools.Requests != nil {
		pools.Requests.Close()
	}
}

func configuredPool(base *pgxpool.Config, connections int32, application string) *pgxpool.Config {
	configuration := base.Copy()
	configuration.MaxConns = connections
	configuration.ConnConfig.Config.RuntimeParams["application_name"] = application
	return configuration
}

func boundedDuration(duration time.Duration) bool { return duration >= time.Millisecond }

func milliseconds(duration time.Duration) string {
	return strconv.FormatInt(duration.Milliseconds(), 10) + "ms"
}
