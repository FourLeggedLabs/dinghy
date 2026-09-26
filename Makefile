
BINARY ?= dinghy
VERSION ?= $(shell git describe --tags --always --dirty)

all: build test

# Build the Dinghy binary
build: dinghy.go cmd pkg
	@go build -v -ldflags "-X main.VERSION=$(VERSION)" -o $(BINARY) .

# Test this project
test: build
	@go test -v -race -covermode atomic -coverprofile=profile.cov ./...

# Run go vet across the codebase
vet:
	@go vet ./...

# Format all Go code
format:
	@gofmt -l -w .

run: build
	@./$(BINARY)

# Remove build artifacts from the working tree
clean:
	@rm -f ./$(BINARY) profile.cov
