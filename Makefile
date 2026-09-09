.PHONY: benchmark-check benchmark-postgres-check dev-contract down generate generate-check help integration integration-contract logs openapi-source-update openapi-source-verify reset smoke status test up

DEV_STACK := ./scripts/dev-stack.sh

help:
	@printf '%s\n' \
		'Local Docker stack:' \
		'  make up                 Build, initialize, and start DANS' \
		'  make smoke              Exercise delegation, DNS, and audit end to end' \
		'  make status             Show containers and DANS readiness' \
		'  make logs               Follow stack logs' \
		'  make down               Stop containers and preserve local data' \
		'  make reset CONFIRM=1    Delete the local stack, data, and credentials' \
		'' \
		'Development:' \
		'  make test               Run unit tests' \
		'  make integration        Run the full Docker integration suite' \
		'  make generate-check     Check generated files for drift' \
		'  make dev-contract       Check the local-stack documentation contract'

up:
	@$(DEV_STACK) up

down:
	@$(DEV_STACK) down

status:
	@$(DEV_STACK) status

logs:
	@$(DEV_STACK) logs

smoke:
	@$(DEV_STACK) smoke

reset:
	@CONFIRM='$(CONFIRM)' $(DEV_STACK) reset

dev-contract:
	@./scripts/dev-contract.sh

benchmark-check:
	go test ./internal/identifier ./internal/dnsname ./internal/httpapi ./internal/httpserver ./internal/upstream -run '^$$' -bench '^Benchmark' -benchtime=1x -benchmem

benchmark-postgres-check:
	@test -n "$$DANS_TEST_DATABASE_URL" || { echo "benchmark-postgres-check requires DANS_TEST_DATABASE_URL" >&2; exit 2; }
	go test -tags=integration ./internal/database ./internal/httpserver -run '^$$' -bench '^Benchmark(Authorize|Authenticate|RuntimeCompatibility|Authorization|DNSAudit|ApplicationPostgres)' -benchtime=1x -benchmem

generate: openapi-source-verify
	go tool sqlc generate -f sqlc.yaml
	go run ./internal/cmd/openapibundle -source api/openapi/vendor/powerdns-authoritative-5.1.3.yaml -overlay api/openapi/dans-overlay.yaml -output api/openapi/openapi.json
	go tool oapi-codegen --config api/openapi/oapi-codegen.yaml api/openapi/openapi.json

generate-check:
	@tmp=$$(mktemp -d); \
	trap 'rm -rf "$$tmp"' EXIT HUP INT TERM; \
	cp api/openapi/openapi.json "$$tmp/openapi.json"; \
	cp api/openapi.gen.go "$$tmp/openapi.gen.go"; \
	cp internal/database/db.go "$$tmp/db.go"; \
	cp internal/database/models.go "$$tmp/models.go"; \
	cp internal/database/resources.sql.go "$$tmp/resources.sql.go"; \
	$(MAKE) --no-print-directory generate; \
	cmp -s "$$tmp/openapi.json" api/openapi/openapi.json && \
	cmp -s "$$tmp/openapi.gen.go" api/openapi.gen.go && \
	cmp -s "$$tmp/db.go" internal/database/db.go && \
	cmp -s "$$tmp/models.go" internal/database/models.go && \
	cmp -s "$$tmp/resources.sql.go" internal/database/resources.sql.go || { \
		echo "generated artifacts are stale; run make generate" >&2; \
		diff -u "$$tmp/openapi.json" api/openapi/openapi.json || true; \
		diff -u "$$tmp/openapi.gen.go" api/openapi.gen.go || true; \
		diff -u "$$tmp/db.go" internal/database/db.go || true; \
		diff -u "$$tmp/models.go" internal/database/models.go || true; \
		diff -u "$$tmp/resources.sql.go" internal/database/resources.sql.go || true; \
		exit 1; \
	}

integration-contract:
	./scripts/integration-contract.sh

integration:
	./scripts/integration.sh

openapi-source-update:
	./scripts/powerdns-openapi.sh update

openapi-source-verify:
	./scripts/powerdns-openapi.sh verify

test:
	go test ./...
