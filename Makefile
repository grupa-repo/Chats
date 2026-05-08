DEV_ENV_SETUP_FOLDER ?= ./dev-env
DOCKER_COMPOSE_FILE ?= $(DEV_ENV_SETUP_FOLDER)/docker-compose.yml
CONTAINER_NAME ?= "chat-containers"
VERSION ?= $(shell git rev-parse --short HEAD)

help:
	@echo "make version to get the current version"
	@echo "make start to start go-api server"
	@echo "make build"
	@echo "make rebuild-docker"
	@echo "make logs"
	@echo "make down to remove docker containers"
	@echo "make test to run the unit test"

version:
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
