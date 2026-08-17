## Purpose

Defines the stable command, configuration, secret-handling, release, and real-system verification contracts used to operate and test DANS.

## Requirements

### Requirement: One executable covers supported workflows
DANS SHALL distribute one executable whose command surface covers serving the API, database migration and status, bootstrap and recovery, restore finalization, DANS resource management, audit export, health, version, shell completion, and common zone and RRset workflows. Help, version, and completion MUST run without configuration, credentials, network access, or other side effects.

#### Scenario: Offline informational command
- **WHEN** a caller requests help, version, or shell completion with no configuration file or credentials
- **THEN** the command succeeds without contacting DANS, PostgreSQL, or PowerDNS

#### Scenario: Maintenance command uses the release artifact
- **WHEN** an operator invokes migration, bootstrap, recovery, or restore finalization from the distributed executable
- **THEN** the selected maintenance workflow runs without requiring a separate administrative binary

### Requirement: Configuration file selection is explicit
Each invocation SHALL optionally read one JSON configuration file selected by `--config` or `DANS_CONFIG`, with `--config` taking precedence when both are present. DANS MUST NOT search implicit locations or load an alternate configuration format, and an explicitly named file that is missing, unreadable, malformed, or not JSON MUST cause a configuration failure.

#### Scenario: No file is named
- **WHEN** neither `--config` nor `DANS_CONFIG` is set
- **THEN** the command resolves configuration from built-in defaults, documented environment variables, and flags without searching the filesystem

#### Scenario: Flag selects the file
- **WHEN** `DANS_CONFIG` names one file and `--config` names another
- **THEN** only the file named by `--config` is loaded

#### Scenario: Named file cannot be loaded
- **WHEN** the selected file is absent, unreadable, malformed, or uses a non-JSON format
- **THEN** the command performs no requested operation and exits with code 2

### Requirement: Configuration precedence is deterministic
For every non-secret setting, DANS SHALL resolve values in ascending precedence from built-in defaults, the selected JSON file, a documented explicitly bound `DANS_*` environment variable, and a command flag. Only documented environment names MUST affect configuration, and an empty environment value SHALL be treated as unset.

#### Scenario: Every source defines one setting
- **WHEN** a setting has a built-in default and is also present in the file, its bound environment variable, and a flag
- **THEN** the flag value is used

#### Scenario: Environment overrides the file
- **WHEN** a setting is present in the selected file and its bound environment variable but not a flag
- **THEN** the environment value is used

#### Scenario: Unknown environment name is present
- **WHEN** the process environment contains an undocumented `DANS_*` name
- **THEN** that name does not create or change a configuration setting

### Requirement: Configuration is strict, command-scoped, and immutable
DANS SHALL reject unknown JSON properties and values of the wrong type, then MUST validate the required settings for only the selected command. A long-running command SHALL freeze its resolved configuration at startup and MUST NOT apply later file or environment changes until it is restarted.

#### Scenario: Unknown configuration property
- **WHEN** the selected JSON file contains an unknown property
- **THEN** the command performs no requested operation and exits with code 2

#### Scenario: Online command has only client settings
- **WHEN** an online management command has valid client configuration but no server, database, or PowerDNS settings
- **THEN** it can run without reporting unrelated missing settings

#### Scenario: Configuration changes after startup
- **WHEN** an operator changes the selected file or process environment after `serve` has completed startup
- **THEN** the running server retains its original resolved settings until restart

### Requirement: Secrets use only approved sources
A secret SHALL be accepted only from its dedicated environment variable or a mutually exclusive `*_file` setting and MUST NOT be accepted as a literal command flag or JSON value. The PowerDNS key SHALL additionally accept one explicitly named rendered fragment containing exactly one plaintext `api-key` assignment; DANS MUST NOT discover it by searching general PowerDNS configuration paths.

File-backed secrets MUST be read once, support mounted or symlinked secret files, remove one terminal LF or CRLF, reject empty or malformed values, and remain redacted from command output and logs.

#### Scenario: Environment secret is supplied
- **WHEN** a required secret has a non-empty value in its dedicated environment variable and no competing file source
- **THEN** the command uses that value without printing it

#### Scenario: File secret is supplied
- **WHEN** a required secret is in a configured regular or symlinked secret file ending in one newline
- **THEN** the command removes that terminal newline and uses the remaining non-empty value

#### Scenario: Secret sources conflict
- **WHEN** both the dedicated environment value and `*_file` source are configured for the same secret
- **THEN** the command performs no requested operation and exits with code 2

#### Scenario: Literal secret is attempted
- **WHEN** a caller places a secret value in a JSON property or literal command flag
- **THEN** configuration is rejected or the unsupported flag is rejected with exit code 2

#### Scenario: Rendered PowerDNS fragment is ambiguous
- **WHEN** the explicitly named rendered fragment has zero or multiple plaintext `api-key` assignments or otherwise cannot yield one key
- **THEN** DANS rejects the source as a configuration failure

### Requirement: CLI output is deterministic and stream-safe
Supported commands SHALL offer deterministic `text` and `json` output, with JSON as the stable automation contract. Successful data MUST be written only to stdout, diagnostics and failures MUST be written only to stderr, and audit streaming export SHALL use one complete JSON object per NDJSON line. A runtime or API failure MUST NOT append command usage to the diagnostic.

#### Scenario: JSON success
- **WHEN** a command succeeds with JSON output selected
- **THEN** stdout contains the documented deterministic JSON result and stderr is empty

#### Scenario: Runtime failure
- **WHEN** a syntactically valid command fails because of a runtime or API error
- **THEN** stderr contains one diagnostic, stdout contains no success payload, and command usage is not appended

#### Scenario: Audit export streams records
- **WHEN** an operator exports more than one audit record as NDJSON
- **THEN** stdout contains one complete JSON audit object per line without surrounding array syntax

### Requirement: Exit statuses have stable meanings
The CLI SHALL exit with code 0 for success, 1 for runtime or API failure, 2 for invocation or configuration failure, and 130 when interrupted. Commands MUST NOT prompt interactively, and an operation requiring explicit destructive confirmation SHALL make no change when that confirmation is absent.

#### Scenario: Successful command
- **WHEN** a command completes its requested operation successfully
- **THEN** it exits with code 0

#### Scenario: API failure
- **WHEN** a valid online command receives an API or transport failure
- **THEN** it exits with code 1

#### Scenario: Invocation failure
- **WHEN** arguments or configuration are invalid
- **THEN** the command exits with code 2

#### Scenario: Interrupted command
- **WHEN** the caller interrupts a running command
- **THEN** it stops through cancellation and exits with code 130

#### Scenario: Destructive confirmation is absent
- **WHEN** a destructive offline command is invoked without its required explicit confirmation
- **THEN** it exits with code 2 without changing durable state and without prompting

### Requirement: Online commands dogfood the public API
Every online CLI workflow SHALL communicate only through DANS's documented public API and MUST NOT access server handlers, PostgreSQL, or the PowerDNS listener directly. The CLI SHALL cover every DANS management workflow and common zone and RRset operations, while the full PowerDNS compatibility surface remains available through HTTP and the published client rather than a handwritten clone or generic raw-request command.

#### Scenario: Online management from a separate host
- **WHEN** a caller runs a configured online management command on a host with access only to the public DANS endpoint
- **THEN** the workflow succeeds without database or direct PowerDNS connectivity

#### Scenario: Unsupported rare PowerDNS operation
- **WHEN** a caller needs a compatibility operation without a dedicated CLI command
- **THEN** the operation remains available through the documented HTTP API and published client, and no raw-request escape command is required

### Requirement: Every change passes real-system integration QA
Every change MUST pass a required Linux integration gate that builds the release executable and starts fresh real PostgreSQL, two same-version DANS instances, and PowerDNS 5.1.3 services. The gate SHALL exercise the current minor releases of the oldest and newest supported PostgreSQL majors, run migration and bootstrap through the compiled CLI, drive management and DNS workflows only through public interfaces, and verify results with actual DNS queries and upstream state.

The gate MUST cover direct and group grants, forward and PTR RRsets, apex and literal-wildcard protection, whole-batch denial, cross-instance revocation, sensitive-route denial, audit outcomes, dependency outages, schema mismatch, and ambiguous upstream results.

#### Scenario: Public behavior passes on the support matrix
- **WHEN** a proposed change passes all required workflows against current-minor PostgreSQL 16 and 18 with two DANS instances and PowerDNS 5.1.3
- **THEN** the real-system integration gate succeeds

#### Scenario: Cross-instance security regresses
- **WHEN** a revocation committed through one instance is not enforced by the other instance in the integration environment
- **THEN** the required gate fails and the change cannot be accepted

#### Scenario: Dependency outage behavior regresses
- **WHEN** an injected PostgreSQL or PowerDNS outage causes authorization bypass, mutation replay, or an incorrect readiness signal
- **THEN** the required gate fails and the change cannot be accepted

### Requirement: Fault controls remain outside production
Integration QA SHALL inject outages and ambiguous results only through test-environment process, container, or network controls. Production artifacts MUST NOT expose seed, reset, introspection, or fault-injection endpoints used solely by tests.

#### Scenario: Integration fault is injected
- **WHEN** the required gate simulates a dependency outage or ambiguous upstream result
- **THEN** the fault is produced outside the production API surface

#### Scenario: Production routes are enumerated
- **WHEN** the production OpenAPI contract and executable are inspected
- **THEN** they contain no test-only seed, reset, introspection, or fault route

### Requirement: Release artifacts support the documented deployment targets
Each release SHALL publish checksummed Linux amd64 and arm64 executables and a non-root OCI image containing the same command surface for serving and maintenance. DANS MUST provide minimal Docker-compatible and Kubernetes deployment examples that keep PostgreSQL and PowerDNS as external dependencies rather than bundling them.

#### Scenario: OCI image runs without root
- **WHEN** an operator starts the published OCI image with its default runtime identity
- **THEN** the DANS command surface runs as a non-root user

#### Scenario: Maintenance runs from the image
- **WHEN** an operator invokes a migration or recovery subcommand from the published image
- **THEN** the same image executes that workflow without a second artifact

#### Scenario: Published binary integrity is checked
- **WHEN** an operator verifies a Linux amd64 or arm64 executable against the release checksum
- **THEN** the checksum identifies the published artifact exactly
