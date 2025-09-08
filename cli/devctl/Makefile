BIN_DIR := ../../kit/bin
BIN := $(BIN_DIR)/devctl

.PHONY: build clean test

build:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BIN) ./

test:
	go test ./...

clean:
	rm -f $(BIN)

