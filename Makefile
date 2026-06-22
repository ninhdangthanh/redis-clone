APP_NAME := redis-clone
BIN_DIR := bin
BIN := $(BIN_DIR)/redis-server
AIR_BIN := $(BIN_DIR)/air
DOCKER_IMAGE := redis-clone:latest
DOCKER_COMPOSE ?= docker compose
GOCACHE ?= $(CURDIR)/.gocache

.PHONY: help run dev build test clean docker-build compose-up compose-down compose-logs compose-ps compose-restart

help:
	@echo "Redis clone commands:"
	@echo "  make run              Run locally with go run ./cmd"
	@echo "  make dev              Run locally with Air autoreload"
	@echo "  make build            Build local binary at $(BIN)"
	@echo "  make test             Run Go tests"
	@echo "  make clean            Remove local build output"
	@echo "  make docker-build     Build Docker image $(DOCKER_IMAGE)"
	@echo "  make compose-up       Build and start docker compose in background"
	@echo "  make compose-down     Stop and remove docker compose services"
	@echo "  make compose-logs     Follow docker compose logs"
	@echo "  make compose-ps       Show docker compose service status"
	@echo "  make compose-restart  Restart docker compose services"

run:
	GOCACHE=$(GOCACHE) go run ./cmd

dev: $(AIR_BIN)
	$(AIR_BIN) -c .air.toml

$(AIR_BIN):
	mkdir -p $(BIN_DIR)
	GOBIN=$(CURDIR)/$(BIN_DIR) GOCACHE=$(GOCACHE) go install github.com/air-verse/air@latest

build:
	mkdir -p $(BIN_DIR)
	GOCACHE=$(GOCACHE) go build -o $(BIN) ./cmd

test:
	GOCACHE=$(GOCACHE) go test ./...

clean:
	rm -rf $(BIN_DIR) .gocache

docker-build:
	docker build -t $(DOCKER_IMAGE) .

compose-up:
	$(DOCKER_COMPOSE) up --build -d

compose-down:
	$(DOCKER_COMPOSE) down

compose-logs:
	$(DOCKER_COMPOSE) logs -f

compose-ps:
	$(DOCKER_COMPOSE) ps

compose-restart:
	$(DOCKER_COMPOSE) restart
