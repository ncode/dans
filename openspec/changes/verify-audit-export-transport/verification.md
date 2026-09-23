# Audit-export transport verification

- The stalled-reader test uses a real `net/http` server over a backpressured connection. The export stopped at its existing 30-second write deadline; the client observed an incomplete HTTP body, and no later page was read.
- The loopback client-disconnect test canceled the pending second page and stopped further page work. It also passed 20 race-detector repetitions.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, and strict OpenSpec validation passed with the pinned Go toolchain.
- `make integration` passed locally against PostgreSQL 16.14 and 18.4. Its disposable containers and volumes were removed.
- Local OCR delegated rule resolution and independent review accounted for all seven changed files. The one confirmed test-harness finding (an unbounded pre-response wait) was fixed; affected tests and the full race suite passed again.
- No production handler, API, or dependency change was needed. Raw test and container logs are not included.
