BIN_DIR := ../../kit/bin
BIN := $(BIN_DIR)/devctl
NIX ?= nix --extra-experimental-features 'nix-command flakes'

.PHONY: build clean test

build:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BIN) ./

test:
	@fixture="$$($(NIX) build --no-link --print-out-paths --no-write-lock-file '../..#native-reset-test-config')" && \
	  DEVKIT_TEST_NATIVE_RESET_CODEX_CONFIG_SOURCE="$$fixture" go test -count=1 ./...

clean:
	rm -f $(BIN)
