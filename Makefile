BINARY  := ckts
GOFILES := $(wildcard *.go)

# Detect OS for cross-compile targets
UNAME_S := $(shell uname -s)

.PHONY: all build clean tidy run

all: build

build: $(BINARY)

$(BINARY): $(GOFILES) go.mod go.sum
	go build -o $(BINARY) .

# Cross-compile for Raspberry Pi (64-bit ARM Linux)
.PHONY: linux-arm64
linux-arm64:
	GOOS=linux GOARCH=arm64 go build -o $(BINARY)-linux-arm64 .

# Cross-compile for Linux amd64
.PHONY: linux-amd64
linux-amd64:
	GOOS=linux GOARCH=amd64 go build -o $(BINARY)-linux-amd64 .

# Cross-compile for macOS (Apple Silicon)
.PHONY: darwin-arm64
darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build -o $(BINARY)-darwin-arm64 .

# Cross-compile for macOS (Intel)
.PHONY: darwin-amd64
darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build -o $(BINARY)-darwin-amd64 .

tidy:
	go mod tidy

clean:
	rm -f $(BINARY) $(BINARY)-linux-arm64 $(BINARY)-linux-amd64 $(BINARY)-darwin-arm64 $(BINARY)-darwin-amd64

run: build
	./$(BINARY) $(ARGS)
