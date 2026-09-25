## ADDED Requirements

### Requirement: CI rehearses restore finalization against a real database copy
For each supported PostgreSQL major, the required integration gate SHALL back up a populated disposable installation and restore it into an isolated database. The rehearsal MUST keep DANS instances from serving the restored copy until the offline finalization command succeeds. It SHALL verify that existing identity, authorization, and audit data survive; every pre-restore credential captured by the rehearsal is rejected afterward; one replacement credential authenticates as the same enabled operator; and the finalized installation becomes ready for public API access. CI-visible output and artifacts MUST NOT contain backup bytes, credential values, or raw restore diagnostics.

#### Scenario: Restore and finalize a populated installation
- **WHEN** the gate restores a backup containing an operator, another active token, authorization data, and audit history, then finalizes it offline for the existing operator
- **THEN** the restored installation becomes ready with the same operator identity and preserved data, and the replacement credential can use the public API

#### Scenario: Restored credentials are invalidated
- **WHEN** the finalized installation receives any pre-restore credential captured by the rehearsal
- **THEN** it rejects that credential and does not restore its authority

#### Scenario: Finalization does not complete
- **WHEN** offline finalization fails during the rehearsal
- **THEN** the gate fails without starting a DANS instance against that restored copy or publishing confidential diagnostics
