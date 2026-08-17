package cli

import (
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/dnsname"
	"github.com/spf13/cobra"
)

func newZonesCommand(options Options) *cobra.Command {
	zones := &cobra.Command{Use: "zones", Short: "Manage common zone workflows"}
	zones.PersistentFlags().String("server", "localhost", "PowerDNS server ID")

	list := &cobra.Command{
		Use:   "list",
		Short: "List zones",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			params := &api.ListZonesParams{}
			var err error
			params.Zone, err = valueFlag(cmd, "zone")
			if err != nil {
				return err
			}
			params.Dnssec, err = boolFlag(cmd, "dnssec")
			if err != nil {
				return err
			}
			serverID, err := serverID(cmd)
			if err != nil {
				return err
			}
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			response, err := client.ListZonesWithResponse(cmd.Context(), serverID, params)
			if err != nil {
				return transportFailure("list zones", err)
			}
			return emitExpected(cmd, config, "list zones", response, http.StatusOK, response.JSON200)
		},
	}
	list.Flags().String("zone", "", "Filter by canonical zone name")
	list.Flags().Bool("dnssec", false, "Filter by DNSSEC state")

	get := &cobra.Command{
		Use:   "get ZONE_ID",
		Short: "Get a zone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			zoneID, err := checkedZoneID(args[0])
			if err != nil {
				return err
			}
			serverID, err := serverID(cmd)
			if err != nil {
				return err
			}
			rrsets, err := boolFlag(cmd, "rrsets")
			if err != nil {
				return err
			}
			includeDisabled, err := boolFlag(cmd, "include-disabled")
			if err != nil {
				return err
			}
			params := &api.ListZoneParams{Rrsets: rrsets, IncludeDisabled: includeDisabled}
			params.RrsetName, err = valueFlag(cmd, "rrset-name")
			if err != nil {
				return err
			}
			params.RrsetType, err = valueFlag(cmd, "rrset-type")
			if err != nil {
				return err
			}
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			response, err := client.ListZoneWithResponse(cmd.Context(), serverID, zoneID, params)
			if err != nil {
				return transportFailure("get zone", err)
			}
			return emitExpected(cmd, config, "get zone", response, http.StatusOK, response.JSON200)
		},
	}
	get.Flags().Bool("rrsets", false, "Include RRsets")
	get.Flags().String("rrset-name", "", "Filter included RRsets by owner name")
	get.Flags().String("rrset-type", "", "Filter included RRsets by type")
	get.Flags().Bool("include-disabled", false, "Include disabled record values")

	create := dataCommand("create", "Create a zone", func(cmd *cobra.Command, _ []string) error {
		body, err := decodeData[api.Zone](cmd)
		if err != nil {
			return err
		}
		serverID, err := serverID(cmd)
		if err != nil {
			return err
		}
		includeRRSets, err := boolFlag(cmd, "rrsets")
		if err != nil {
			return err
		}
		config, client, err := onlineClient(cmd, options)
		if err != nil {
			return err
		}
		response, err := client.CreateZoneWithResponse(cmd.Context(), serverID, &api.CreateZoneParams{Rrsets: includeRRSets}, body)
		if err != nil {
			return transportFailure("create zone", err)
		}
		return emitExpected(cmd, config, "create zone", response, http.StatusCreated, response.JSON201)
	})
	create.Flags().Bool("rrsets", false, "Include RRsets in the response")

	deleteCommand := &cobra.Command{
		Use:   "delete ZONE_ID",
		Short: "Delete a zone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireConfirmation(cmd); err != nil {
				return err
			}
			zoneID, err := checkedZoneID(args[0])
			if err != nil {
				return err
			}
			serverID, err := serverID(cmd)
			if err != nil {
				return err
			}
			config, client, err := onlineClient(cmd, options)
			if err != nil {
				return err
			}
			response, err := client.DeleteZoneWithResponse(cmd.Context(), serverID, zoneID)
			if err != nil {
				return transportFailure("delete zone", err)
			}
			if err := requireStatus("delete zone", response, http.StatusNoContent); err != nil {
				return err
			}
			return emitSuccess(cmd, config)
		},
	}
	addConfirmationFlag(deleteCommand)

	zones.AddCommand(list, get, create, deleteCommand)
	return zones
}

func newRRSetsCommand(options Options) *cobra.Command {
	rrsets := &cobra.Command{Use: "rrsets", Short: "Manage common RRset workflows"}
	rrsets.PersistentFlags().String("server", "localhost", "PowerDNS server ID")

	list := &cobra.Command{
		Use:   "list ZONE_ID",
		Short: "List a zone's RRsets",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			zone, config, err := getZoneRRsets(cmd, options, args[0], "", "")
			if err != nil {
				return err
			}
			if zone.Rrsets == nil {
				empty := []api.RRSet{}
				return emitAPIResult(cmd, config, empty)
			}
			return emitAPIResult(cmd, config, *zone.Rrsets)
		},
	}
	list.Flags().Bool("include-disabled", false, "Include disabled record values")

	get := &cobra.Command{
		Use:   "get ZONE_ID OWNER TYPE",
		Short: "Get one RRset",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			owner, recordType, err := canonicalRRSet(args[1], args[2])
			if err != nil {
				return err
			}
			zone, config, err := getZoneRRsets(cmd, options, args[0], owner, recordType)
			if err != nil {
				return err
			}
			if zone.Rrsets != nil {
				for _, rrset := range *zone.Rrsets {
					candidate, candidateType, parseErr := canonicalRRSet(rrset.Name, rrset.Type)
					if parseErr == nil && candidate == owner && candidateType == recordType {
						return emitAPIResult(cmd, config, rrset)
					}
				}
			}
			return runtimeFailure(fmt.Errorf("get RRset: DANS API returned no matching RRset"))
		},
	}
	get.Flags().Bool("include-disabled", false, "Include disabled record values")

	apply := dataCommand("apply ZONE_ID", "Apply a typed RRset batch", func(cmd *cobra.Command, args []string) error {
		zoneID, err := checkedZoneID(args[0])
		if err != nil {
			return err
		}
		body, err := decodeData[api.ZonePatch](cmd)
		if err != nil {
			return err
		}
		return runZonePatch(cmd, options, zoneID, body, "apply RRset batch")
	})
	apply.Args = cobra.ExactArgs(1)

	replace := &cobra.Command{
		Use:   "replace ZONE_ID OWNER TYPE",
		Short: "Replace an RRset with complete values",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			zoneID, err := checkedZoneID(args[0])
			if err != nil {
				return err
			}
			owner, recordType, err := canonicalRRSet(args[1], args[2])
			if err != nil {
				return err
			}
			ttl, err := requiredTTL(cmd)
			if err != nil {
				return err
			}
			values, err := cmd.Flags().GetStringArray("value")
			if err != nil {
				return invocationFailure(fmt.Errorf("read --value: %w", err))
			}
			if len(values) == 0 {
				return invocationFailure(fmt.Errorf("at least one --value is required"))
			}
			records := make([]api.Record, len(values))
			for index, value := range values {
				records[index] = api.Record{Content: value}
			}
			change := api.RRSetChange{Changetype: api.RRSetChangeChangetypeREPLACE, Name: owner, Type: recordType, Ttl: &ttl, Records: &records}
			return runZonePatch(cmd, options, zoneID, api.ZonePatch{Rrsets: []api.RRSetChange{change}}, "replace RRset")
		},
	}
	replace.Flags().Int("ttl", 0, "RRset TTL in seconds")
	replace.Flags().StringArray("value", nil, "Record value; repeat for multiple values")

	addValue := valueChangeCommand("add-value", "Extend an RRset by one value", api.RRSetChangeChangetypeEXTEND, options)
	addValue.Flags().Int("ttl", 0, "Optional RRset TTL in seconds")
	addValue.Flags().Bool("disabled", false, "Add the value disabled")
	removeValue := valueChangeCommand("remove-value", "Prune one value from an RRset", api.RRSetChangeChangetypePRUNE, options)
	removeValue.Flags().Int("ttl", 0, "Optional RRset TTL in seconds")

	deleteCommand := &cobra.Command{
		Use:   "delete ZONE_ID OWNER TYPE",
		Short: "Delete an RRset",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireConfirmation(cmd); err != nil {
				return err
			}
			zoneID, err := checkedZoneID(args[0])
			if err != nil {
				return err
			}
			owner, recordType, err := canonicalRRSet(args[1], args[2])
			if err != nil {
				return err
			}
			change := api.RRSetChange{Changetype: api.RRSetChangeChangetypeDELETE, Name: owner, Type: recordType}
			return runZonePatch(cmd, options, zoneID, api.ZonePatch{Rrsets: []api.RRSetChange{change}}, "delete RRset")
		},
	}
	addConfirmationFlag(deleteCommand)

	rrsets.AddCommand(list, get, apply, replace, addValue, removeValue, deleteCommand)
	return rrsets
}

func valueChangeCommand(name, short string, changeType api.RRSetChangeChangetype, options Options) *cobra.Command {
	return &cobra.Command{
		Use:   name + " ZONE_ID OWNER TYPE VALUE",
		Short: short,
		Args:  cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			zoneID, err := checkedZoneID(args[0])
			if err != nil {
				return err
			}
			owner, recordType, err := canonicalRRSet(args[1], args[2])
			if err != nil {
				return err
			}
			record := api.Record{Content: args[3]}
			if changeType == api.RRSetChangeChangetypeEXTEND {
				record.Disabled, err = boolFlag(cmd, "disabled")
				if err != nil {
					return err
				}
			}
			records := []api.Record{record}
			change := api.RRSetChange{Changetype: changeType, Name: owner, Type: recordType, Records: &records}
			if cmd.Flags().Lookup("ttl") != nil && cmd.Flags().Changed("ttl") {
				ttl, err := cmd.Flags().GetInt("ttl")
				if err != nil {
					return invocationFailure(fmt.Errorf("read --ttl: %w", err))
				}
				if ttl <= 0 {
					return invocationFailure(fmt.Errorf("--ttl must be positive"))
				}
				change.Ttl = &ttl
			}
			return runZonePatch(cmd, options, zoneID, api.ZonePatch{Rrsets: []api.RRSetChange{change}}, short)
		},
	}
}

func runZonePatch(cmd *cobra.Command, options Options, zoneID string, body api.ZonePatch, operation string) error {
	serverID, err := serverID(cmd)
	if err != nil {
		return err
	}
	config, client, err := onlineClient(cmd, options)
	if err != nil {
		return err
	}
	response, err := client.PatchZoneWithResponse(cmd.Context(), serverID, zoneID, body)
	if err != nil {
		return transportFailure(operation, err)
	}
	if err := requireStatus(operation, response, http.StatusNoContent); err != nil {
		return err
	}
	return emitSuccess(cmd, config)
}

func getZoneRRsets(cmd *cobra.Command, options Options, zoneValue, owner, recordType string) (*api.Zone, Config, error) {
	zoneID, err := checkedZoneID(zoneValue)
	if err != nil {
		return nil, Config{}, err
	}
	serverID, err := serverID(cmd)
	if err != nil {
		return nil, Config{}, err
	}
	include := true
	params := &api.ListZoneParams{Rrsets: &include}
	if owner != "" {
		params.RrsetName = &owner
		params.RrsetType = &recordType
	}
	params.IncludeDisabled, err = boolFlag(cmd, "include-disabled")
	if err != nil {
		return nil, Config{}, err
	}
	config, client, err := onlineClient(cmd, options)
	if err != nil {
		return nil, Config{}, err
	}
	response, err := client.ListZoneWithResponse(cmd.Context(), serverID, zoneID, params)
	if err != nil {
		return nil, Config{}, transportFailure("get zone RRsets", err)
	}
	if err := requireStatus("get zone RRsets", response, http.StatusOK); err != nil {
		return nil, Config{}, err
	}
	if response.JSON200 == nil {
		return nil, Config{}, runtimeFailure(fmt.Errorf("get zone RRsets: API response did not contain the declared JSON body"))
	}
	return response.JSON200, config, nil
}

func serverID(cmd *cobra.Command) (string, error) {
	value, err := cmd.Flags().GetString("server")
	if err != nil {
		return "", invocationFailure(fmt.Errorf("read --server: %w", err))
	}
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return "", invocationFailure(fmt.Errorf("--server must be non-empty"))
	}
	return value, nil
}

func checkedZoneID(value string) (string, error) {
	if value == "" || len(value) > 255 || strings.ContainsAny(value, "\x00\r\n") {
		return "", invocationFailure(fmt.Errorf("invalid zone ID"))
	}
	return value, nil
}

func canonicalRRSet(ownerValue, typeValue string) (string, string, error) {
	owner, err := dnsname.Parse(ownerValue)
	if err != nil {
		return "", "", invocationFailure(fmt.Errorf("invalid RRset owner name: %w", err))
	}
	recordType := strings.ToUpper(typeValue)
	if recordType == "" {
		return "", "", invocationFailure(fmt.Errorf("record type is required"))
	}
	for _, value := range recordType {
		if !unicode.IsUpper(value) && !unicode.IsDigit(value) && value != '-' {
			return "", "", invocationFailure(fmt.Errorf("invalid record type"))
		}
	}
	return owner.String(), recordType, nil
}

func requiredTTL(cmd *cobra.Command) (int, error) {
	ttl, err := cmd.Flags().GetInt("ttl")
	if err != nil {
		return 0, invocationFailure(fmt.Errorf("read --ttl: %w", err))
	}
	if ttl <= 0 {
		return 0, invocationFailure(fmt.Errorf("--ttl must be positive"))
	}
	return ttl, nil
}

func addConfirmationFlag(command *cobra.Command) {
	command.Flags().Bool("confirm", false, "Confirm the destructive operation")
}

func requireConfirmation(command *cobra.Command) error {
	confirmed, err := command.Flags().GetBool("confirm")
	if err != nil {
		return invocationFailure(fmt.Errorf("read --confirm: %w", err))
	}
	if !confirmed {
		return invocationFailure(fmt.Errorf("--confirm is required for this destructive operation"))
	}
	return nil
}
