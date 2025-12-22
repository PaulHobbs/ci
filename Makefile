.PHONY: proto build test clean run

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
