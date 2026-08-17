package database

import (
	"strings"
	"testing"
	"time"
)

func TestRuntimePoolConfigsAreBoundedPrimaryOnlyAndReserveCapacity(t *testing.T) {
	t.Parallel()

	configs, err := RuntimePoolConfigs(RuntimePoolConfig{
		ConnectionString:      "postgres://dans:secret@db.example/dans?sslmode=require&application_name=caller",
		MaxConnections:        12,
		AuthenticationReserve: 3,
		ReadinessReserve:      1,
		ConnectTimeout:        2 * time.Second,
		StatementTimeout:      750 * time.Millisecond,
		LockTimeout:           125 * time.Millisecond,
		TransactionTimeout:    4 * time.Second,
	})
	if err != nil {
		t.Fatalf("RuntimePoolConfigs: %v", err)
	}

	tests := []struct {
		name            string
		config          poolConfiguration
		wantConnections int32
		wantApplication string
	}{
		{name: "requests", config: configs.Requests, wantConnections: 8, wantApplication: "dans-requests"},
		{name: "authentication", config: configs.Authentication, wantConnections: 3, wantApplication: "dans-authentication"},
		{name: "readiness", config: configs.Readiness, wantConnections: 1, wantApplication: "dans-readiness"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configuration := tt.config.config
			if configuration.MaxConns != tt.wantConnections {
				t.Errorf("MaxConns = %d, want %d", configuration.MaxConns, tt.wantConnections)
			}
			if configuration.MinConns != 0 || configuration.MinIdleConns != 0 {
				t.Errorf("unexpected unbounded minimums: MinConns=%d MinIdleConns=%d", configuration.MinConns, configuration.MinIdleConns)
			}
			if configuration.ConnConfig.ConnectTimeout != 2*time.Second {
				t.Errorf("ConnectTimeout = %s", configuration.ConnConfig.ConnectTimeout)
			}
			if configuration.ConnConfig.Config.ValidateConnect == nil {
				t.Error("primary/read-write validation is not configured")
			}
			params := configuration.ConnConfig.Config.RuntimeParams
			want := map[string]string{
				"application_name":                    tt.wantApplication,
				"statement_timeout":                   "750ms",
				"lock_timeout":                        "125ms",
				"idle_in_transaction_session_timeout": "4000ms",
			}
			for name, value := range want {
				if params[name] != value {
					t.Errorf("RuntimeParams[%q] = %q, want %q", name, params[name], value)
				}
			}
		})
	}
}

func TestRuntimePoolConfigRejectsUnboundedOrUnsafeValuesWithoutLeakingDSN(t *testing.T) {
	t.Parallel()

	valid := RuntimePoolConfig{
		ConnectionString:      "postgres://dans:do-not-print@db.example/dans",
		MaxConnections:        6,
		AuthenticationReserve: 2,
		ReadinessReserve:      1,
		ConnectTimeout:        time.Second,
		StatementTimeout:      time.Second,
		LockTimeout:           time.Second,
		TransactionTimeout:    time.Second,
	}
	tests := []struct {
		name   string
		mutate func(*RuntimePoolConfig)
	}{
		{name: "missing DSN", mutate: func(config *RuntimePoolConfig) { config.ConnectionString = "" }},
		{name: "malformed DSN", mutate: func(config *RuntimePoolConfig) { config.ConnectionString = "://do-not-print" }},
		{name: "no request capacity", mutate: func(config *RuntimePoolConfig) { config.MaxConnections = 3 }},
		{name: "no authentication reserve", mutate: func(config *RuntimePoolConfig) { config.AuthenticationReserve = 0 }},
		{name: "no readiness reserve", mutate: func(config *RuntimePoolConfig) { config.ReadinessReserve = 0 }},
		{name: "negative reserve", mutate: func(config *RuntimePoolConfig) { config.AuthenticationReserve = -1 }},
		{name: "zero connect timeout", mutate: func(config *RuntimePoolConfig) { config.ConnectTimeout = 0 }},
		{name: "sub-millisecond statement timeout", mutate: func(config *RuntimePoolConfig) { config.StatementTimeout = time.Nanosecond }},
		{name: "zero lock timeout", mutate: func(config *RuntimePoolConfig) { config.LockTimeout = 0 }},
		{name: "zero transaction timeout", mutate: func(config *RuntimePoolConfig) { config.TransactionTimeout = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config := valid
			tt.mutate(&config)
			_, err := RuntimePoolConfigs(config)
			if err == nil {
				t.Fatal("RuntimePoolConfigs() error = nil")
			}
			if strings.Contains(err.Error(), "do-not-print") {
				t.Errorf("error leaked DSN: %v", err)
			}
		})
	}
}
