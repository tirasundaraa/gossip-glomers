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

# Challenge 2: Unique IDs
# -w unique-ids: The workload to run
# --bin: Path to your binary
# --node-count 3: 3 nodes needed for Unique IDs
# --time-limit 30: Run for 30 seconds
# --rate 1000: 1000 messages per second
# --availability total: All nodes are available
# --nemesis partition: Partition the network
challenge-2: build
	$(MAELSTROM_BINARY) test -w unique-ids --bin $(BUILD_DIR)/$(BINARY_NAME) --node-count 3 --time-limit 30 --rate 1000 --availability total --nemesis partition

# Challenge 3a: Broadcast
# Makefile
challenge-3a: build
	./maelstrom/maelstrom test -w broadcast --bin ./bin/node --node-count 1 --time-limit 20 --rate 10

# Challenge 3b
challenge-3b: build
	./maelstrom/maelstrom test -w broadcast --bin ./bin/node --node-count 5 --time-limit 20 --rate 10

# Makefile for 3c (Notice the partition nemesis!)
challenge-3c: build
	./maelstrom/maelstrom test -w broadcast --bin ./bin/node --node-count 5 --time-limit 20 --rate 10 --nemesis partition

# Challenge 3d (efficiency)
challenge-3d: build
	./maelstrom/maelstrom test -w broadcast --bin ./bin/node --node-count 25 --time-limit 20 --rate 100 --latency 100

# Challenge 3e (efficiency part-2)
challenge-3e: build
	./maelstrom/maelstrom test -w broadcast --bin ./bin/node --node-count 25 --time-limit 20 --rate 100 --latency 100

# Helper to view the logs if something crashes
# Maelstrom logs are in store/<test-name>/node-logs/
clean:
	rm -rf $(BUILD_DIR) store/
