## Context

See `proposal.md`. The export handler sets a 30-second per-page write deadline and aborts a started response with `http.ErrAbortHandler`. Current tests prove page traversal and aborted reads over `httptest.NewServer`, but stalled writes and client cancellation are exercised with a fake response writer. The existing `audit-zone-lifecycle` spec already defines the required behavior.

## Goals / Non-Goals

**Goals:** Verify the actual `net/http` response path under backpressure and disconnect, including bounded termination, no later page read, and an incomplete client-visible body.

**Non-Goals:** New export options, changed timeouts, a cross-protocol matrix, or a new production configuration seam for tests.

## Decisions

- Use a standard-library `net.Pipe` connection served by `http.Server` for the stalled reader. It provides deterministic backpressure without depending on a host TCP send-buffer size; read the response headers, pause body reads, then verify that the existing write deadline closes the stream with an incomplete body. A loopback TCP test that must fill platform-dependent socket buffers would be less reliable.
- Use `httptest.NewServer` and a real HTTP client for disconnect: read the first emitted page, close the response body while a later page is pending, and verify the handler's request context is canceled before further page work. Reuse the existing fake store/authentication patterns.
- Keep the production 30-second deadline unchanged. The stalled test measures the real bound rather than injecting a shorter test-only timeout; keep it focused so the extra wall time remains predictable.
- If either test fails, correct the shared export path and rerun both new and existing export checks. Do not alter production code merely to accommodate the harness.

## Risks / Trade-offs

- Real deadline measurement adds roughly 30 seconds per relevant test run → run one controlled stall case, enforce a generous outer test bound, and keep it out of unrelated lifecycle scripts.
- `net.Pipe` is not a kernel TCP socket → use it only for deterministic write backpressure; the separate loopback HTTP test covers actual client disconnect.
- A slow CI runner can cross a tight elapsed-time assertion → assert eventual termination within an outer margin rather than an exact timestamp.
