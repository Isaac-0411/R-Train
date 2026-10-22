BINARY  := bin
DB_PATH ?= data/app.db

# Settings of the local test setup, see "make dev".
DEV_DB         ?= data/dev.db
DEV_ADMIN_USER ?= admin
DEV_ADMIN_PASS ?= local-admin-123

.PHONY: help build server adminctl run dev dev-seed test vet fmt tidy clean docker docker-run

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## Build the server and the admin command
	@mkdir -p $(BINARY)
	go build -trimpath -o $(BINARY)/server ./cmd/server
	go build -trimpath -o $(BINARY)/adminctl ./cmd/adminctl

server: build ## Build only the web server
	@mkdir -p $(BINARY)
	go build -trimpath -o $(BINARY)/server ./cmd/server

adminctl: ## Build only the admin command
	@mkdir -p $(BINARY)
	go build -trimpath -o $(BINARY)/adminctl ./cmd/adminctl

run: ## Run the web server
	go run ./cmd/server

dev-seed: ## Fill the local test database with demo content
	DB_PATH=$(DEV_DB) go run ./cmd/adminctl seed-demo \
		--username $(DEV_ADMIN_USER) --admin-password $(DEV_ADMIN_PASS)

dev: dev-seed ## Prepare a local test database and run the server on it
	DB_PATH=$(DEV_DB) go run ./cmd/server

test: ## Run all tests
	go test ./... -count=1

test-race: ## Run all tests with the race detector
	go test ./... -count=1 -race

vet: ## Run go vet
	go vet ./...

fmt: ## Format the code
	gofmt -w .

tidy: ## Tidy the module files
	go mod tidy

clean: ## Remove the build artefacts
	rm -rf $(BINARY)

docker: ## Build the Docker image
	docker build -t reading-trainer:latest .

docker-run: ## Run the Docker image on port 8080
	docker run --rm -p 8080:8080 -v "$(PWD)/data:/data" reading-trainer:latest
