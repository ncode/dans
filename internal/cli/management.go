package cli

import (
	"fmt"
	"net/http"
	"unicode/utf8"

	"github.com/ncode/dans/api"
	"github.com/spf13/cobra"
)

func newManagementCommands(options Options) []*cobra.Command {
	return []*cobra.Command{
		newIdentitiesCommand(options),
		newGroupsCommand(options),
		newDelegationsCommand(options),
		newBindingsCommand(options),
		newAuditCommand(options),
		newMeCommand(options),
		newZonesCommand(options),
		newRRSetsCommand(options),
		newHealthCommand(options),
		newDocsCommand(options),
	}
}

func newIdentitiesCommand(options Options) *cobra.Command {
	identities := &cobra.Command{Use: "identities", Short: "Manage identities"}

	list := &cobra.Command{
		Use:   "list",
		Short: "List identities",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			limit, cursor, err := pageFlags(cmd)
			if err != nil {
				return err
			}
			params := &api.ListIdentitiesParams{Limit: limit, Cursor: cursor}
			kind, err := valueFlag(cmd, "kind")
			if err != nil {
				return err
			}
			if kind != nil {
				value := api.ListIdentitiesParamsKind(*kind)
				if value != "user" && value != "service" {
					return invocationFailure(fmt.Errorf("--kind must be user or service"))
				}
				params.Kind = &value
			}
			params.Enabled, err = boolFlag(cmd, "enabled")
			if err != nil {
				return err
			}
			params.Operator, err = boolFlag(cmd, "operator")
			if err != nil {
				return err
			}
			handle, err := valueFlag(cmd, "handle")
			if err != nil {
				return err
			}
			if handle != nil {
				value := api.Handle(*handle)
				params.Handle = &value
			}
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			response, err := client.ListIdentitiesWithResponse(cmd.Context(), params)
			if err != nil {
				return transportFailure("list identities", err)
			}
			return emitExpected(cmd, config, "list identities", response, http.StatusOK, response.JSON200)
		},
	}
	addPageFlags(list)
	list.Flags().String("kind", "", "Identity kind: user or service")
	list.Flags().Bool("enabled", false, "Filter by enabled state")
	list.Flags().Bool("operator", false, "Filter by operator state")
	list.Flags().String("handle", "", "Filter by exact handle")

	create := dataCommand("create", "Create an identity", func(cmd *cobra.Command, _ []string) error {
		body, err := decodeData[api.IdentityCreate](cmd)
		if err != nil {
			return err
		}
		config, client, err := onlineClient(cmd, options)
		if err != nil {
			return err
		}
		response, err := client.CreateIdentityWithResponse(cmd.Context(), body)
		if err != nil {
			return transportFailure("create identity", err)
		}
		return emitExpected(cmd, config, "create identity", response, http.StatusCreated, response.JSON201)
	})

	get := &cobra.Command{
		Use:   "get ID",
		Short: "Get an identity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseResourceID(args[0])
			if err != nil {
				return err
			}
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			response, err := client.GetIdentityWithResponse(cmd.Context(), id)
			if err != nil {
				return transportFailure("get identity", err)
			}
			return emitExpected(cmd, config, "get identity", response, http.StatusOK, response.JSON200)
		},
	}

	update := dataCommand("update ID", "Update an identity", func(cmd *cobra.Command, args []string) error {
		id, err := parseResourceID(args[0])
		if err != nil {
			return err
		}
		body, err := decodeData[api.IdentityPatch](cmd)
		if err != nil {
			return err
		}
		config, client, err := onlineClient(cmd, options)
		if err != nil {
			return err
		}
		response, err := client.UpdateIdentityWithResponse(cmd.Context(), id, body)
		if err != nil {
			return transportFailure("update identity", err)
		}
		return emitExpected(cmd, config, "update identity", response, http.StatusOK, response.JSON200)
	})
	update.Args = cobra.ExactArgs(1)

	identities.AddCommand(list, create, get, update, newIdentityTokensCommand(options))
	return identities
}

func newIdentityTokensCommand(options Options) *cobra.Command {
	tokens := &cobra.Command{Use: "tokens", Short: "Manage an identity's API tokens"}

	list := &cobra.Command{
		Use:   "list IDENTITY_ID",
		Short: "List an identity's tokens",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseResourceID(args[0])
			if err != nil {
				return err
			}
			limit, cursor, err := pageFlags(cmd)
			if err != nil {
				return err
			}
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			response, err := client.ListIdentityTokensWithResponse(cmd.Context(), id, &api.ListIdentityTokensParams{Limit: limit, Cursor: cursor})
			if err != nil {
				return transportFailure("list identity tokens", err)
			}
			return emitExpected(cmd, config, "list identity tokens", response, http.StatusOK, response.JSON200)
		},
	}
	addPageFlags(list)

	create := dataCommand("create IDENTITY_ID", "Create an identity token", func(cmd *cobra.Command, args []string) error {
		id, err := parseResourceID(args[0])
		if err != nil {
			return err
		}
		body, err := decodeData[api.TokenCreate](cmd)
		if err != nil {
			return err
		}
		config, client, err := onlineClient(cmd, options)
		if err != nil {
			return err
		}
		response, err := client.CreateIdentityTokenWithResponse(cmd.Context(), id, body)
		if err != nil {
			return transportFailure("create identity token", err)
		}
		return emitExpected(cmd, config, "create identity token", response, http.StatusCreated, response.JSON201)
	})
	create.Args = cobra.ExactArgs(1)

	revoke := &cobra.Command{
		Use:   "revoke IDENTITY_ID TOKEN_ID",
		Short: "Revoke an identity token",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			identityID, err := parseResourceID(args[0])
			if err != nil {
				return err
			}
			tokenID, err := parseResourceID(args[1])
			if err != nil {
				return err
			}
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			response, err := client.RevokeIdentityTokenWithResponse(cmd.Context(), identityID, tokenID)
			if err != nil {
				return transportFailure("revoke identity token", err)
			}
			if err := requireStatus("revoke identity token", response, http.StatusNoContent); err != nil {
				return err
			}
			return emitSuccess(cmd, config)
		},
	}

	tokens.AddCommand(list, create, revoke)
	return tokens
}

func newGroupsCommand(options Options) *cobra.Command {
	groups := &cobra.Command{Use: "groups", Short: "Manage groups"}

	list := &cobra.Command{
		Use:   "list",
		Short: "List groups",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			limit, cursor, err := pageFlags(cmd)
			if err != nil {
				return err
			}
			params := &api.ListGroupsParams{Limit: limit, Cursor: cursor}
			params.Enabled, err = boolFlag(cmd, "enabled")
			if err != nil {
				return err
			}
			handle, err := valueFlag(cmd, "handle")
			if err != nil {
				return err
			}
			if handle != nil {
				value := api.Handle(*handle)
				params.Handle = &value
			}
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			response, err := client.ListGroupsWithResponse(cmd.Context(), params)
			if err != nil {
				return transportFailure("list groups", err)
			}
			return emitExpected(cmd, config, "list groups", response, http.StatusOK, response.JSON200)
		},
	}
	addPageFlags(list)
	list.Flags().Bool("enabled", false, "Filter by enabled state")
	list.Flags().String("handle", "", "Filter by exact handle")

	create := dataCommand("create", "Create a group", func(cmd *cobra.Command, _ []string) error {
		body, err := decodeData[api.GroupCreate](cmd)
		if err != nil {
			return err
		}
		config, client, err := onlineClient(cmd, options)
		if err != nil {
			return err
		}
		response, err := client.CreateGroupWithResponse(cmd.Context(), body)
		if err != nil {
			return transportFailure("create group", err)
		}
		return emitExpected(cmd, config, "create group", response, http.StatusCreated, response.JSON201)
	})

	get := resourceGetCommand("get ID", "Get a group", options, func(cmd *cobra.Command, client *api.DANSClientWithResponses, id api.ResourceID) (apiResponse, any, error) {
		response, err := client.GetGroupWithResponse(cmd.Context(), id)
		if response == nil {
			return response, nil, err
		}
		return response, response.JSON200, err
	})

	update := dataCommand("update ID", "Update a group", func(cmd *cobra.Command, args []string) error {
		id, err := parseResourceID(args[0])
		if err != nil {
			return err
		}
		body, err := decodeData[api.GroupPatch](cmd)
		if err != nil {
			return err
		}
		config, client, err := onlineClient(cmd, options)
		if err != nil {
			return err
		}
		response, err := client.UpdateGroupWithResponse(cmd.Context(), id, body)
		if err != nil {
			return transportFailure("update group", err)
		}
		return emitExpected(cmd, config, "update group", response, http.StatusOK, response.JSON200)
	})
	update.Args = cobra.ExactArgs(1)

	groups.AddCommand(list, create, get, update, newGroupMembersCommand(options))
	return groups
}

func newGroupMembersCommand(options Options) *cobra.Command {
	members := &cobra.Command{Use: "members", Short: "Manage direct group members"}
	list := &cobra.Command{
		Use:   "list GROUP_ID",
		Short: "List direct group members",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			groupID, err := parseResourceID(args[0])
			if err != nil {
				return err
			}
			limit, cursor, err := pageFlags(cmd)
			if err != nil {
				return err
			}
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			response, err := client.ListGroupMembersWithResponse(cmd.Context(), groupID, &api.ListGroupMembersParams{Limit: limit, Cursor: cursor})
			if err != nil {
				return transportFailure("list group members", err)
			}
			return emitExpected(cmd, config, "list group members", response, http.StatusOK, response.JSON200)
		},
	}
	addPageFlags(list)
	add := membershipChangeCommand("add GROUP_ID IDENTITY_ID", "Add a direct group member", options, true)
	remove := membershipChangeCommand("remove GROUP_ID IDENTITY_ID", "Remove a direct group member", options, false)
	members.AddCommand(list, add, remove)
	return members
}

func membershipChangeCommand(use, short string, options Options, add bool) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			groupID, err := parseResourceID(args[0])
			if err != nil {
				return err
			}
			identityID, err := parseResourceID(args[1])
			if err != nil {
				return err
			}
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			operation := "remove group member"
			var response apiResponse
			if add {
				operation = "add group member"
				response, err = client.AddGroupMemberWithResponse(cmd.Context(), groupID, identityID)
			} else {
				response, err = client.RemoveGroupMemberWithResponse(cmd.Context(), groupID, identityID)
			}
			if err != nil {
				return transportFailure(operation, err)
			}
			if err := requireStatus(operation, response, http.StatusNoContent); err != nil {
				return err
			}
			return emitSuccess(cmd, config)
		},
	}
}

func newDelegationsCommand(options Options) *cobra.Command {
	delegations := &cobra.Command{Use: "delegations", Short: "Manage DNS delegations"}
	list := &cobra.Command{
		Use:   "list",
		Short: "List delegations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			limit, cursor, err := pageFlags(cmd)
			if err != nil {
				return err
			}
			params := &api.ListDelegationsParams{Limit: limit, Cursor: cursor}
			params.ZoneBindingId, err = resourceIDFlag(cmd, "zone-binding")
			if err != nil {
				return err
			}
			params.GranteeId, err = resourceIDFlag(cmd, "grantee")
			if err != nil {
				return err
			}
			params.Active, err = boolFlag(cmd, "active")
			if err != nil {
				return err
			}
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			response, err := client.ListDelegationsWithResponse(cmd.Context(), params)
			if err != nil {
				return transportFailure("list delegations", err)
			}
			return emitExpected(cmd, config, "list delegations", response, http.StatusOK, response.JSON200)
		},
	}
	addPageFlags(list)
	list.Flags().String("zone-binding", "", "Filter by zone-binding resource ID")
	list.Flags().String("grantee", "", "Filter by grantee resource ID")
	list.Flags().Bool("active", false, "Filter by active state")

	create := dataCommand("create", "Create a delegation", func(cmd *cobra.Command, _ []string) error {
		body, err := decodeData[api.DelegationCreate](cmd)
		if err != nil {
			return err
		}
		config, client, err := onlineClient(cmd, options)
		if err != nil {
			return err
		}
		response, err := client.CreateDelegationWithResponse(cmd.Context(), body)
		if err != nil {
			return transportFailure("create delegation", err)
		}
		return emitExpected(cmd, config, "create delegation", response, http.StatusCreated, response.JSON201)
	})
	get := delegationGetCommand(options)
	revoke := resourceNoContentCommand("revoke ID", "Revoke a delegation", "revoke delegation", options, func(cmd *cobra.Command, client *api.DANSClientWithResponses, id api.ResourceID) (apiResponse, error) {
		return client.RevokeDelegationWithResponse(cmd.Context(), id)
	})
	delegations.AddCommand(list, create, get, revoke)
	return delegations
}

func delegationGetCommand(options Options) *cobra.Command {
	return &cobra.Command{
		Use:   "get ID",
		Short: "Get a delegation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseResourceID(args[0])
			if err != nil {
				return err
			}
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			response, err := client.GetDelegationWithResponse(cmd.Context(), id)
			if err != nil {
				return transportFailure("get delegation", err)
			}
			return emitExpected(cmd, config, "get delegation", response, http.StatusOK, response.JSON200)
		},
	}
}

func newBindingsCommand(options Options) *cobra.Command {
	bindings := &cobra.Command{Use: "bindings", Aliases: []string{"reconcile"}, Short: "Manage and reconcile zone bindings"}
	list := &cobra.Command{
		Use:   "list",
		Short: "List zone bindings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			limit, cursor, err := pageFlags(cmd)
			if err != nil {
				return err
			}
			params := &api.ListZoneBindingsParams{Limit: limit, Cursor: cursor}
			status, err := valueFlag(cmd, "status")
			if err != nil {
				return err
			}
			if status != nil {
				value := api.ListZoneBindingsParamsStatus(*status)
				if !value.Valid() {
					return invocationFailure(fmt.Errorf("invalid --status value"))
				}
				params.Status = &value
			}
			params.ZoneName, err = valueFlag(cmd, "zone-name")
			if err != nil {
				return err
			}
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			response, err := client.ListZoneBindingsWithResponse(cmd.Context(), params)
			if err != nil {
				return transportFailure("list zone bindings", err)
			}
			return emitExpected(cmd, config, "list zone bindings", response, http.StatusOK, response.JSON200)
		},
	}
	addPageFlags(list)
	list.Flags().String("status", "", "Filter by binding status")
	list.Flags().String("zone-name", "", "Filter by canonical zone name")

	create := bindingBodyCommand("create", "Create a zone binding", "create zone binding", options, false)
	get := bindingGetCommand(options)
	observe := bindingResultCommand("observe ID", "Observe upstream zone state", "observe zone binding", options, func(cmd *cobra.Command, client *api.DANSClientWithResponses, id api.ResourceID) (*api.ObserveZoneBindingResponse, error) {
		return client.ObserveZoneBindingWithResponse(cmd.Context(), id)
	}, func(response *api.ObserveZoneBindingResponse) *api.ZoneObservation { return response.JSON200 })
	confirmAbsent := bindingResultCommand("confirm-absent ID", "Confirm upstream zone absence", "confirm zone absence", options, func(cmd *cobra.Command, client *api.DANSClientWithResponses, id api.ResourceID) (*api.ConfirmZoneBindingAbsentResponse, error) {
		return client.ConfirmZoneBindingAbsentWithResponse(cmd.Context(), id)
	}, func(response *api.ConfirmZoneBindingAbsentResponse) *api.ZoneBinding { return response.JSON200 })
	retryDelete := resourceNoContentCommand("retry-delete ID", "Retry one retired-zone deletion", "retry zone deletion", options, func(cmd *cobra.Command, client *api.DANSClientWithResponses, id api.ResourceID) (apiResponse, error) {
		return client.RetryZoneBindingDeletionWithResponse(cmd.Context(), id)
	})
	rebind := bindingBodyCommand("rebind ID", "Bind a new zone lifetime", "rebind zone", options, true)
	bindings.AddCommand(list, create, get, observe, confirmAbsent, retryDelete, rebind)
	return bindings
}

func bindingGetCommand(options Options) *cobra.Command {
	return &cobra.Command{
		Use:   "get ID",
		Short: "Get a zone binding",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseResourceID(args[0])
			if err != nil {
				return err
			}
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			response, err := client.GetZoneBindingWithResponse(cmd.Context(), id)
			if err != nil {
				return transportFailure("get zone binding", err)
			}
			return emitExpected(cmd, config, "get zone binding", response, http.StatusOK, response.JSON200)
		},
	}
}

func bindingBodyCommand(use, short, operation string, options Options, needsID bool) *cobra.Command {
	command := dataCommand(use, short, func(cmd *cobra.Command, args []string) error {
		body, err := decodeData[api.ZoneBindingCreate](cmd)
		if err != nil {
			return err
		}
		var id api.ResourceID
		if needsID {
			id, err = parseResourceID(args[0])
			if err != nil {
				return err
			}
		}
		config, client, err := onlineClient(cmd, options)
		if err != nil {
			return err
		}
		var response *api.CreateZoneBindingResponse
		if needsID {
			rebound, callErr := client.RebindZoneBindingWithResponse(cmd.Context(), id, body)
			if callErr != nil {
				return transportFailure(operation, callErr)
			}
			return emitExpected(cmd, config, operation, rebound, http.StatusCreated, rebound.JSON201)
		}
		response, err = client.CreateZoneBindingWithResponse(cmd.Context(), body)
		if err != nil {
			return transportFailure(operation, err)
		}
		return emitExpected(cmd, config, operation, response, http.StatusCreated, response.JSON201)
	})
	if needsID {
		command.Args = cobra.ExactArgs(1)
	}
	return command
}

func bindingResultCommand[R apiResponse, T any](use, short, operation string, options Options, call func(*cobra.Command, *api.DANSClientWithResponses, api.ResourceID) (R, error), body func(R) *T) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseResourceID(args[0])
			if err != nil {
				return err
			}
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			response, err := call(cmd, client, id)
			if err != nil {
				return transportFailure(operation, err)
			}
			return emitExpected(cmd, config, operation, response, http.StatusOK, body(response))
		},
	}
}

func newAuditCommand(options Options) *cobra.Command {
	audit := &cobra.Command{Use: "audit", Short: "Read the immutable audit history"}
	list := auditListCommand(options, false)
	list.Use = "list"
	list.Short = "List audit events"
	export := auditListCommand(options, true)
	export.Use = "export"
	export.Short = "Stream all matching audit events as NDJSON"
	audit.AddCommand(list, export)
	return audit
}

func auditListCommand(options Options, stream bool) *cobra.Command {
	command := &cobra.Command{
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			params, err := auditParams(cmd)
			if err != nil {
				return err
			}
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			if !stream {
				response, err := client.ListAuditEventsWithResponse(cmd.Context(), params)
				if err != nil {
					return transportFailure("list audit events", err)
				}
				return emitExpected(cmd, config, "list audit events", response, http.StatusOK, response.JSON200)
			}
			for {
				response, err := client.ListAuditEventsWithResponse(cmd.Context(), params)
				if err != nil {
					return transportFailure("export audit events", err)
				}
				if err := requireStatus("export audit events", response, http.StatusOK); err != nil {
					return err
				}
				if response.JSON200 == nil {
					return runtimeFailure(fmt.Errorf("export audit events: API response did not contain the declared JSON body"))
				}
				for i := range response.JSON200.Items {
					if err := writeNDJSON(cmd.OutOrStdout(), response.JSON200.Items[i]); err != nil {
						return runtimeFailure(fmt.Errorf("export audit events: %w", err))
					}
				}
				if response.JSON200.NextCursor.IsNull() {
					return nil
				}
				nextCursor, err := response.JSON200.NextCursor.Get()
				if err != nil || nextCursor == "" {
					return runtimeFailure(fmt.Errorf("export audit events: API response contained an invalid next cursor"))
				}
				cursor := api.Cursor(nextCursor)
				params.Cursor = &cursor
			}
		},
	}
	addPageFlags(command)
	command.Flags().String("actor", "", "Filter by actor resource ID")
	command.Flags().String("action", "", "Filter by action")
	command.Flags().String("target-type", "", "Filter by target type")
	command.Flags().String("target", "", "Filter by opaque audit target ID")
	command.Flags().String("result", "", "Filter by result")
	return command
}

func auditParams(cmd *cobra.Command) (*api.ListAuditEventsParams, error) {
	limit, cursor, err := pageFlags(cmd)
	if err != nil {
		return nil, err
	}
	params := &api.ListAuditEventsParams{Limit: limit, Cursor: cursor}
	params.ActorId, err = resourceIDFlag(cmd, "actor")
	if err != nil {
		return nil, err
	}
	params.TargetId, err = valueFlag(cmd, "target")
	if err != nil {
		return nil, err
	}
	if params.TargetId != nil && utf8.RuneCountInString(*params.TargetId) > 1024 {
		return nil, invocationFailure(fmt.Errorf("--target must be at most 1024 characters"))
	}
	params.Action, err = valueFlag(cmd, "action")
	if err != nil {
		return nil, err
	}
	params.TargetType, err = valueFlag(cmd, "target-type")
	if err != nil {
		return nil, err
	}
	params.Result, err = valueFlag(cmd, "result")
	if err != nil {
		return nil, err
	}
	return params, nil
}

func newMeCommand(options Options) *cobra.Command {
	me := &cobra.Command{Use: "me", Short: "Use self-service identity workflows"}
	get := meGetCommand(options)
	groups := mePageCommand("groups", "List current identity groups", options, func(cmd *cobra.Command, client *api.DANSClientWithResponses, limit *api.Limit, cursor *api.Cursor) (apiResponse, any, error) {
		response, err := client.ListCurrentIdentityGroupsWithResponse(cmd.Context(), &api.ListCurrentIdentityGroupsParams{Limit: limit, Cursor: cursor})
		if response == nil {
			return response, nil, err
		}
		return response, response.JSON200, err
	})
	delegations := mePageCommand("delegations", "List current identity delegations", options, func(cmd *cobra.Command, client *api.DANSClientWithResponses, limit *api.Limit, cursor *api.Cursor) (apiResponse, any, error) {
		response, err := client.ListCurrentIdentityDelegationsWithResponse(cmd.Context(), &api.ListCurrentIdentityDelegationsParams{Limit: limit, Cursor: cursor})
		if response == nil {
			return response, nil, err
		}
		return response, response.JSON200, err
	})
	tokens := mePageCommand("tokens", "List current identity tokens", options, func(cmd *cobra.Command, client *api.DANSClientWithResponses, limit *api.Limit, cursor *api.Cursor) (apiResponse, any, error) {
		response, err := client.ListCurrentIdentityTokensWithResponse(cmd.Context(), &api.ListCurrentIdentityTokensParams{Limit: limit, Cursor: cursor})
		if response == nil {
			return response, nil, err
		}
		return response, response.JSON200, err
	})
	tokenCreate := dataCommand("token-create", "Create a token for the current identity", func(cmd *cobra.Command, _ []string) error {
		body, err := decodeData[api.TokenCreate](cmd)
		if err != nil {
			return err
		}
		config, client, err := onlineClient(cmd, options)
		if err != nil {
			return err
		}
		response, err := client.CreateCurrentIdentityTokenWithResponse(cmd.Context(), body)
		if err != nil {
			return transportFailure("create current identity token", err)
		}
		return emitExpected(cmd, config, "create current identity token", response, http.StatusCreated, response.JSON201)
	})
	tokenRevoke := resourceNoContentCommand("token-revoke TOKEN_ID", "Revoke a current-identity token", "revoke current identity token", options, func(cmd *cobra.Command, client *api.DANSClientWithResponses, id api.ResourceID) (apiResponse, error) {
		return client.RevokeCurrentIdentityTokenWithResponse(cmd.Context(), id)
	})
	me.AddCommand(get, groups, delegations, tokens, tokenCreate, tokenRevoke)
	return me
}

func meGetCommand(options Options) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Get the current identity",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			response, err := client.GetCurrentIdentityWithResponse(cmd.Context())
			if err != nil {
				return transportFailure("get current identity", err)
			}
			return emitExpected(cmd, config, "get current identity", response, http.StatusOK, response.JSON200)
		},
	}
}

type mePageCall func(*cobra.Command, *api.DANSClientWithResponses, *api.Limit, *api.Cursor) (apiResponse, any, error)

func mePageCommand(use, short string, options Options, call mePageCall) *cobra.Command {
	command := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			limit, cursor, err := pageFlags(cmd)
			if err != nil {
				return err
			}
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			response, body, err := call(cmd, client, limit, cursor)
			if err != nil {
				return transportFailure(short, err)
			}
			if err := requireStatus(short, response, http.StatusOK); err != nil {
				return err
			}
			if body == nil {
				return runtimeFailure(fmt.Errorf("%s: API response did not contain the declared JSON body", short))
			}
			return emitAPIResult(cmd, config, body)
		},
	}
	addPageFlags(command)
	return command
}

func dataCommand(use, short string, run func(*cobra.Command, []string) error) *cobra.Command {
	command := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs, RunE: run}
	addDataFlag(command)
	return command
}

func resourceNoContentCommand(use, short, operation string, options Options, call func(*cobra.Command, *api.DANSClientWithResponses, api.ResourceID) (apiResponse, error)) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseResourceID(args[0])
			if err != nil {
				return err
			}
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			response, err := call(cmd, client, id)
			if err != nil {
				return transportFailure(operation, err)
			}
			if err := requireStatus(operation, response, http.StatusNoContent); err != nil {
				return err
			}
			return emitSuccess(cmd, config)
		},
	}
}

func resourceIDFlag(command *cobra.Command, name string) (*api.ResourceID, error) {
	value, err := command.Flags().GetString(name)
	if err != nil {
		return nil, invocationFailure(fmt.Errorf("read --%s: %w", name, err))
	}
	if value == "" {
		return nil, nil
	}
	id, err := parseResourceID(value)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

type resourceGetCall func(*cobra.Command, *api.DANSClientWithResponses, api.ResourceID) (apiResponse, any, error)

func resourceGetCommand(use, short string, options Options, call resourceGetCall) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseResourceID(args[0])
			if err != nil {
				return err
			}
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			response, body, err := call(cmd, client, id)
			if err != nil {
				return transportFailure(short, err)
			}
			if err := requireStatus(short, response, http.StatusOK); err != nil {
				return err
			}
			if body == nil {
				return runtimeFailure(fmt.Errorf("%s: API response did not contain the declared JSON body", short))
			}
			return emitAPIResult(cmd, config, body)
		},
	}
}
