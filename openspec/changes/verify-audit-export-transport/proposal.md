## Why

Audit export promises to stop stalled or disconnected transfers and never make a partial download appear complete. Existing tests cover cancellation and simulated write failures, but the real stalled-client deadline has not been measured over an HTTP connection.

## What Changes

- Add a real-connection regression check for a client that stops reading an in-progress audit export; verify the bounded write deadline ends the transfer and prevents further page work.
- Check a disconnected client through the same transport path and confirm an interrupted response cannot be read as a complete export.
- Correct the handler only if the checks reveal a defect. Preserve the existing export API, filters, authorization, and NDJSON format.

## Capabilities

No new or modified capability requirements. The existing `audit-zone-lifecycle` interrupt-export scenario already specifies the intended behavior; this change closes a verification gap. Specs are skipped for this change.

## Impact

Focused HTTP server tests and, only if necessary, audit-export transport handling. No new dependency or public API change.
