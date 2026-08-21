# ROADPASS developer entry points. The full stack runs on the local Docker engine
# (OrbStack); the Go binary and dashboard also run natively against `make infra`.
SHELL := bash
DC    := docker compose -f deploy/compose.yml
SCALE ?= 1

.PHONY: help web build check test infra infra-down dev stack seed e2e debug debug-down logs stack-down stack-clean

help: ## list targets
	@grep -hE '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

web: ## build the dashboard into internal/api/dist
	cd web && npm ci --no-audit --no-fund && npm run build

build: ## build bin/roadpass (embeds the last dashboard build)
	go build -o bin/roadpass ./cmd/roadpass

check: ## gofmt, vet, Go tests, chaincode tests, TypeScript
	@test -z "$$(gofmt -l cmd internal chaincode)" || { gofmt -l cmd internal chaincode; exit 1; }
	go vet ./...
	go test ./...
	cd chaincode/roadpass-proof && go test ./...
	cd web && npx tsc -b --noEmit

test: check ## alias for check

infra: ## Postgres, ClickHouse, Redis for native development (ports 15433/19100/16379)
	docker compose -f deploy/compose.infra.yml up -d --wait

infra-down: ## stop the native-development data services
	docker compose -f deploy/compose.infra.yml down

dev: ## rebuild and (re)start the native server on http://127.0.0.1:8090
	scripts/dev-restart.sh

stack: ## build and start the full stack, seed it, seal on Fabric (http://localhost:8080)
	$(DC) build fabric app
	$(DC) up -d --wait postgres clickhouse redis fabric
	SEED_SCALE=$(SCALE) $(DC) --profile seed run --rm seed
	$(DC) up -d --wait app worker

seed: ## re-seed the running stack (SCALE=1 ≈ 2.9M rows)
	SEED_SCALE=$(SCALE) $(DC) --profile seed run --rm seed

e2e: ## full stack + Playwright, leaves the stack running
	scripts/e2e.sh --keep

debug: ## Hyperledger Explorer on http://localhost:8091 (debugging only)
	$(DC) --profile debug up -d explorer
	@echo "Explorer: http://localhost:8091"

debug-down: ## stop Explorer
	$(DC) --profile debug stop explorer explorer-db
	$(DC) --profile debug rm -f explorer explorer-db explorer-config

logs: ## follow app and worker logs
	$(DC) logs -f app worker

stack-down: ## stop the stack, keep its data
	$(DC) --profile debug --profile e2e down

stack-clean: ## stop the stack and delete its volumes (ledger included)
	$(DC) --profile debug --profile e2e --profile seed down -v
