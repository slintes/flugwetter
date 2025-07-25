# Makefile for flugwetter project
# Uses podman for container operations

# Image name and tag
IMAGE_NAME = quay.io/slintes/flugwetter

# Default target: build and run
.PHONY: all
all: build run

# Build the container image
.PHONY: build
build:
	podman build -t $(IMAGE_NAME) .

# Run the container
.PHONY: run
run:
	podman run -p 8080:8080 $(IMAGE_NAME)

# Push the image to registry
.PHONY: push
push:
	podman push $(IMAGE_NAME)

# Help target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  all    - Build image and run container (default)"
	@echo "  build  - Build container image"
	@echo "  run    - Run container"
	@echo "  push   - Push image to registry"
	@echo "  help   - Show this help message"