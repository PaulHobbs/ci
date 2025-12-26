.PHONY: proto build build-ci build-web build-all test clean run run-ci ci dev-web

# Proto generation
proto:
	protoc \
		--proto_path=proto \
		--proto_path=$(shell go env GOPATH)/src \
		--go_out=gen --go_opt=paths=source_relative \
		--go-grpc_out=gen --go-grpc_opt=paths=source_relative \
		proto/turboci/v1/*.proto

# Build the server
build:
	go build -o bin/turboci-server ./cmd/server

# Build CI tools
build-ci: build
	go build -o bin/ci ./cmd/ci
	go build -o bin/trigger-runner ./cmd/ci/runners/trigger
	go build -o bin/materialize-runner ./cmd/ci/runners/materialize
	go build -o bin/builder-runner ./cmd/ci/runners/builder
	go build -o bin/tester-runner ./cmd/ci/runners/tester
	go build -o bin/conditional-runner ./cmd/ci/runners/conditional
	go build -o bin/collector-runner ./cmd/ci/runners/collector

# Run tests
test:
	go test -v ./...
	@for dir in cmd/*/; do \
		if [ -f "$$dir/go.mod" ]; then \
			echo "Testing $$dir..."; \
			(cd "$$dir" && go test -v ./...); \
		fi \
	done

# Clean build artifacts
clean:
	rm -rf bin/
	rm -rf gen/proto/

# Run the server
run: build
	./bin/turboci-server

# Run CI (builds and tests this repo using turboci-lite)
run-ci: build-ci
	./bin/ci --verbose

# Run CI using go run (builds and tests everything)
ci:
	go run cmd/ci/main.go

# Install protoc plugins
install-tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Format code
fmt:
	go fmt ./...

# Lint code
lint:
	go vet ./...

# Build web frontend
build-web:
	cd web && pnpm install && pnpm run build

# Run web frontend development server (with hot reload)
dev-web:
	cd web && pnpm install && pnpm run dev

# Build everything including web
build-all: build-web build
