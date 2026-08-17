package cli

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ncode/dans/api"
	"github.com/spf13/cobra"
)

func newHealthCommand(options Options) *cobra.Command {
	health := &cobra.Command{Use: "health", Short: "Check DANS liveness and readiness"}
	health.AddCommand(
		healthCheckCommand("live", "Check process liveness", "check liveness", "live", options, func(cmd *cobra.Command, client *api.DANSClientWithResponses) (apiResponse, error) {
			return client.GetLivenessWithResponse(cmd.Context())
		}),
		healthCheckCommand("ready", "Check service readiness", "check readiness", "ready", options, func(cmd *cobra.Command, client *api.DANSClientWithResponses) (apiResponse, error) {
			return client.GetReadinessWithResponse(cmd.Context())
		}),
	)
	return health
}

func healthCheckCommand(use, short, operation, status string, options Options, call func(*cobra.Command, *api.DANSClientWithResponses) (apiResponse, error)) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			config, client, err := rootAPIClient(cmd, options, false)
			if err != nil {
				return err
			}
			response, err := call(cmd, client)
			if err != nil {
				return transportFailure(operation, err)
			}
			if err := requireStatus(operation, response, http.StatusOK); err != nil {
				return err
			}
			value := struct {
				Status string `json:"status"`
			}{Status: status}
			return emitAPIResult(cmd, config, value)
		},
	}
}

func newDocsCommand(options Options) *cobra.Command {
	return &cobra.Command{
		Use:   "docs",
		Short: "Download the authenticated combined OpenAPI document",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			config, client, err := rootAPIClient(cmd, options, true)
			if err != nil {
				return err
			}
			response, err := client.GetAPIDocumentWithResponse(cmd.Context())
			if err != nil {
				return transportFailure("download API document", err)
			}
			return emitExpected(cmd, config, "download API document", response, http.StatusOK, response.JSON200)
		},
	}
}

func rootAPIClient(cmd *cobra.Command, options Options, authenticated bool) (Config, *api.DANSClientWithResponses, error) {
	config, err := loadConfig(cmd, configScopeOnline)
	if err != nil {
		return Config{}, nil, invocationFailure(err)
	}
	doer, err := clientDoer(config, options.HTTPClient)
	if err != nil {
		return Config{}, nil, invocationFailure(err)
	}
	clientOptions := []api.ClientOption{api.WithHTTPClient(doer)}
	if authenticated {
		token, err := loadAPIToken(config)
		if err != nil {
			return Config{}, nil, invocationFailure(err)
		}
		clientOptions = append(clientOptions, api.WithRequestEditorFn(func(_ context.Context, request *http.Request) error {
			request.Header.Set("X-API-Key", token.Value())
			return nil
		}))
	}
	client, err := api.NewDANSClientWithResponses(config.Endpoint, clientOptions...)
	if err != nil {
		return Config{}, nil, invocationFailure(fmt.Errorf("configure DANS root API client: %w", err))
	}
	return config, client, nil
}
