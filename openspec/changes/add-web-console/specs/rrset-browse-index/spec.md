## Purpose

Support complete, bounded browsing of large DNS zones through an API-populated read index without changing authoritative DNS state or delegated write authority.

## ADDED Requirements

### Requirement: Complete bounded RRset browsing
Authenticated identities SHALL be able to browse all ordinary DNS data for 400 zones, including one zone containing 250,000 RRsets, without entering a search term first. A browse page SHALL contain at most 100 complete RRsets ordered by canonical owner name and then record type, with a nullable continuation cursor. Clients MUST NOT download the entire large zone to obtain or filter a page, and requesting each page MUST NOT cause another full-zone upstream read.

#### Scenario: Browse a large zone
- **WHEN** an authenticated caller opens the indexed 250,000-RRset zone
- **THEN** the first page contains at most 100 complete RRsets and a continuation cursor
- **AND** following pages uses bounded browse responses rather than repeated whole-zone transfers

#### Scenario: RRset contains multiple values
- **WHEN** a returned RRset contains multiple record values or comments
- **THEN** the representation includes the complete RRset rather than treating capped search results as its complete contents

#### Scenario: No delegation is present
- **WHEN** an authenticated identity with no write delegation requests browse data
- **THEN** ordinary DNS read visibility is unchanged

### Requirement: Indexed owner-name and type filters
Browsing SHALL support exact owner-name and owner-name prefix filters, an optional record-type filter, and stable name/type ordering. Filtering MUST occur before pagination. Cursors MUST be bounded, validated, tied to the zone and filters, and grant no authority. Each page MUST authenticate against current state. A stable data generation MUST be traversable without omissions or duplicates; incompatible or expired generations MUST be reported explicitly so the client can restart.

#### Scenario: Filter a large zone
- **WHEN** a caller supplies an exact or prefix owner-name filter and a record type
- **THEN** every returned RRset satisfies both filters and continuation retains those filters
- **AND** arbitrary substring or record-value searching is not silently substituted for the requested match mode

#### Scenario: Invalid continuation
- **WHEN** a cursor is malformed, oversized, altered, used for another zone or filter, or refers to an unavailable generation
- **THEN** the API returns an explicit error instead of a misleading empty or unrelated page

### Requirement: API-only rebuildable browsing data
All indexed DNS content SHALL originate in PowerDNS HTTP API reads. The application MUST NOT connect directly to the PowerDNS database or treat indexed data as authoritative state or policy. Data collection MUST have bounded work and buffering, and an incomplete or failed refresh MUST NOT replace the last complete browse generation.

#### Scenario: First access
- **WHEN** a zone has no complete browse data
- **THEN** the API reports indexing in progress and triggers bounded background collection
- **AND** the console shows loading rather than claiming that the zone is empty

#### Scenario: Refresh fails partway through
- **WHEN** a full refresh fails before all RRsets are collected
- **THEN** the previous complete generation is retained and the failure is represented as stale/error state
- **AND** partial results are not published as a complete zone

### Requirement: Incremental mutation freshness
Under healthy dependencies at the supported capacity, successful mutations through any application instance SHALL target visibility in indexed browse data within five seconds, with this target measured by an integration check. Ordinary RRset mutations SHALL refresh their affected complete RRsets rather than rebuild the entire zone. Unknown outcomes MUST trigger reconciliation of reads without replaying writes. Concurrent full and incremental refreshes MUST NOT silently lose an observed mutation or permanently leave stale data marked fresh.

#### Scenario: Write through one instance and browse through another
- **WHEN** a successful CLI or browser RRset mutation completes through one instance
- **THEN** the changed complete RRset becomes visible through another instance within the five-second target in the capacity test
- **AND** the observation path does not issue another DNS mutation

#### Scenario: Mutation races full refresh
- **WHEN** an RRset changes while a whole-zone refresh is being collected
- **THEN** publication preserves or subsequently reconciles that change using durable coordination
- **AND** the change is not discarded with no remaining refresh work

#### Scenario: Mutation outcome is uncertain
- **WHEN** a forwarded mutation has an unknown outcome
- **THEN** affected browse data is treated as requiring read reconciliation and the mutation is never replayed automatically

### Requirement: Full refresh and explicit freshness states
Zones being viewed SHALL be eligible for a full refresh every five minutes, and authenticated users SHALL be able to request a manual refresh. The API SHALL expose enough state to distinguish first indexing, a ready snapshot, an in-progress refresh, and stale data or refresh failure. The schedule MUST NOT be presented as a guarantee that an upstream request has completed successfully. A refresh request MUST NOT expose operational connection details in errors.

#### Scenario: Supported external DNS data changes
- **WHEN** a viewed zone changes through supported DNS transfers or dynamic updates outside the application mutation path
- **THEN** a successful periodic or manual full refresh reflects those records

#### Scenario: Keep prior results visible
- **WHEN** refresh is running or fails after the user has already viewed results
- **THEN** those results can remain visible with their freshness indication and an actionable error on failure
- **AND** live editing still requires a successful authoritative read

### Requirement: Shared refresh coordination and zone lifetime isolation
Refresh scheduling, ownership, and pending mutation work SHALL survive routing across multiple application instances and recover from an interrupted worker. A zone deletion, failed or unknown deletion, or recreation MUST invalidate incompatible browse work and cursors and MUST NOT resurrect retired delegation authority or publish data from a previous zone lifetime as current. Indexing MUST NOT delay initial service readiness by eagerly loading all zones or change the existing fail-closed dependency boundary.

#### Scenario: Two instances open an unindexed zone
- **WHEN** two instances receive simultaneous browse requests for one unindexed zone
- **THEN** they coordinate collection through shared state rather than starting unbounded duplicate full-zone reads

#### Scenario: Worker exits
- **WHEN** the worker holding a refresh claim exits before completion
- **THEN** another healthy instance can recover the bounded read work without issuing or replaying DNS writes

#### Scenario: Recreate a zone
- **WHEN** a supported delete-and-recreate workflow creates a new zone lifetime
- **THEN** previous browse data and refresh workers cannot overwrite the new lifetime and previous delegations remain retired
