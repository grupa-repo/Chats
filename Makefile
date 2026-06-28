DEV_ENV_SETUP_FOLDER ?= ./dev-env
DOCKER_COMPOSE_FILE ?= $(DEV_ENV_SETUP_FOLDER)/docker-compose.yml
CONTAINER_NAME ?= "chat-containers"
VERSION ?= $(shell git rev-parse --short HEAD)
QA_URL ?= https://hp-chat-api-ff9486774a07.herokuapp.com

help:
	@echo "make version        compare local HEAD against the QA deployment"
	@echo "make version-local  print the local git short SHA"
	@echo "make start to start go-api server"
	@echo "make build"
	@echo "make rebuild-docker"
	@echo "make logs"
	@echo "make down to remove docker containers"
	@echo "make test to run the unit test"
	@echo "make test-qa to run the QA WebSocket harness against a deployed env"

version:
	@LOCAL=$$(git rev-parse --short HEAD); \
	DEPLOYED=$$(curl -fsS $(QA_URL)/version | jq -r '.version'); \
	if [ -z "$$DEPLOYED" ] || [ "$$DEPLOYED" = "null" ]; then \
	  echo "could not read deployed version from $(QA_URL)/version"; exit 1; \
	fi; \
	DEPLOYED_SHORT=$$(echo $$DEPLOYED | cut -c1-7); \
	echo "Local:    $$LOCAL"; \
	echo "Deployed: $$DEPLOYED_SHORT  ($(QA_URL))"; \
	if [ "$$LOCAL" = "$$DEPLOYED_SHORT" ]; then \
	  echo "in sync"; \
	else \
	  AHEAD=$$(git log --oneline $$DEPLOYED_SHORT..HEAD 2>/dev/null | wc -l | tr -d ' '); \
	  if [ "$$AHEAD" = "0" ]; then \
	    echo "out of sync (deployed SHA not in local history -- try git fetch)"; \
	  else \
	    echo "out of sync -- local is $$AHEAD commit(s) ahead"; \
	  fi; \
	fi

version-local:
	@echo $(VERSION)

start:
	@echo "Starting app..."
	@docker compose -f $(DOCKER_COMPOSE_FILE) -p $(CONTAINER_NAME) up --build -d

restart:
	make down-hard && make start

down:
	@echo "Stopping app..."
	@docker compose -f $(DOCKER_COMPOSE_FILE) -p $(CONTAINER_NAME) down

down-hard:
	@echo "Stopping app and removing volumes..."
	@docker compose -f $(DOCKER_COMPOSE_FILE) -p $(CONTAINER_NAME) down -v

build:
	go build -v ./...

rebuild-docker:
	@docker compose -f $(DOCKER_COMPOSE_FILE) -p $(CONTAINER_NAME) down
	@docker compose -f $(DOCKER_COMPOSE_FILE) -p $(CONTAINER_NAME) build --no-cache
	@docker compose -f $(DOCKER_COMPOSE_FILE) -p $(CONTAINER_NAME) up -d

watch: start
	@echo "Watching for file changes..."
	@docker compose -f $(DOCKER_COMPOSE_FILE) -p $(CONTAINER_NAME) watch

logs:
	@docker compose -f $(DOCKER_COMPOSE_FILE) -p $(CONTAINER_NAME) logs -f

test:
	go test -v $(shell go list ./... | grep -v integration-tests) -short

# Runs the build-tagged QA harness against a deployed chat service.
# Required env: QA_BASE_URL, QA_WS_URL, QA_JWT_SECRET, QA_DSN.
# Optional:    QA_INTERNAL_TOKEN (required for the resync tests; others skip without it).
# Tests skip with a clear message if any required var is missing.
test-qa:
	go test -tags=qa -v -count=1 ./tests/qa/...
