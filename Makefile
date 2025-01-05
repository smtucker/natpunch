
# Define variables for directories and files
BIN_DIR := bin
PROTO_DIR := proto
PROTO_GEN_DIR := $(PROTO_DIR)/gen/go
SERVER_SRC := cmd/server
CLIENT_SRC := cmd/client

# Define the Go compiler and protoc compiler
GO := go
PROTOC := protoc

# Default target: build all binaries
all: server client

# Rule to build the server binary
server: $(SERVER_SRC) $(PROTO_GEN_DIR)
	$(GO) build -o $(BIN_DIR)/server ./$(SERVER_SRC)

# Rule to build the client binary
client: $(CLIENT_SRC) $(PROTO_GEN_DIR)
	$(GO) build -o $(BIN_DIR)/client ./$(CLIENT_SRC)

# Rule to generate protobuf Go code
$(PROTO_GEN_DIR): $(PROTO_DIR)/api.proto
	mkdir -p $(PROTO_GEN_DIR)
	$(PROTOC) --go_out=$(PROTO_GEN_DIR) -I./proto --go_opt=paths=source_relative api.proto

# Rule to clean up generated files and binaries
clean:
	rm -rf $(BIN_DIR) $(PROTO_GEN_DIR)

# Make sure that the 'all' target is phony so make behaves correctly if a file named all exists
.PHONY: all server client clean
