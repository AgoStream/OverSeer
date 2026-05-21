AGENT_BIN   := bin/overseer-agent
SCHED_BIN   := bin/overseer-scheduler
GO_PKGS     := ./...

.PHONY: build test lint fmt tidy python-lint python-test all

build:
	mkdir -p bin
	go build -o $(AGENT_BIN) ./cmd/agent
	go build -o $(SCHED_BIN) ./cmd/scheduler

test:
	go test $(GO_PKGS)

lint:
	golangci-lint run $(GO_PKGS)

fmt:
	gofumpt -l -w .

tidy:
	go mod tidy

python-lint:
	cd training && ruff check .

python-test:
	cd training && python -m pytest --tb=short

all: fmt lint build test python-lint python-test
