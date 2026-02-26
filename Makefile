# Makefile
BINARY_NAME=node
GO_FILES=./cmd/node
BUILD_DIR=./bin
MAELSTROM_BINARY=./maelstrom/maelstrom

# 1. Build your Go binary
build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) $(GO_FILES)

# 2. Challenge 1: Echo
# -w echo: The workload to run
# --bin: Path to your binary
# --node-count 1: Only 1 node needed for Echo
# --time-limit 10: Run for 10 seconds
challenge-1: build
	$(MAELSTROM_BINARY) test -w echo --bin $(BUILD_DIR)/$(BINARY_NAME) --node-count 1 --time-limit 10

# Helper to view the logs if something crashes
# Maelstrom logs are in store/<test-name>/node-logs/
clean:
	rm -rf $(BUILD_DIR) store/
