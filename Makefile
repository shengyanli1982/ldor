.PHONY: all clean

# Go parameters
BINARY_NAME := ldor
OUTPUT_DIR := bin

all: clean deps build

deps:
	@echo ">>> Downloading dependencies..."
	GO111MODULE=on go mod download

build:
	@echo ">>> Building the binary..."
	GO111MODULE=on CGO_ENABLED=0 go build -tags=jsoniter -ldflags="-w -s" -o $(OUTPUT_DIR)/$(BINARY_NAME)

clean:
	@echo ">>> Cleaning up..."
	rm -f $(OUTPUT_DIR)/$(BINARY_NAME)