.PHONY: build install run-server review tidy test lint docker-build docker-run compose-up compose-down ngrok

BINARY   := ai-code-reviewer
BIN_DIR  := bin
INSTALL  := $(shell go env GOPATH)/bin

# ── Build ─────────────────────────────────────────────────────────────────────

# Build the binary into ./bin/
build:
	@mkdir -p $(BIN_DIR)
	go build -ldflags="-s -w" -o $(BIN_DIR)/$(BINARY) .
	@echo "Built → $(BIN_DIR)/$(BINARY)"

# Install the binary into $GOPATH/bin so it's available system-wide
install:
	go install -ldflags="-s -w" .
	@echo "Installed → $(INSTALL)/$(BINARY)"

tidy:
	go mod tidy

# ── Run ───────────────────────────────────────────────────────────────────────

# Start the webhook server (requires .env)
run-server:
	go run . server

# Review a specific PR in the terminal (usage: make review OWNER=x REPO=y PR=42)
review:
	go run . review --owner $(OWNER) --repo $(REPO) --pr $(PR)

# Review a PR and post inline comments to GitHub
review-post:
	go run . review --owner $(OWNER) --repo $(REPO) --pr $(PR) --post

# ── Test & Lint ───────────────────────────────────────────────────────────────

test:
	go test ./... -v -race -count=1

lint:
	golangci-lint run ./...

# ── Docker ────────────────────────────────────────────────────────────────────

docker-build:
	docker build -t $(BINARY):latest .

docker-run:
	docker run --env-file .env -p 8080:8080 $(BINARY):latest

compose-up:
	docker compose up --build -d

compose-down:
	docker compose down

# ── Local tunnel (requires ngrok) ─────────────────────────────────────────────
ngrok:
	ngrok http 8080
