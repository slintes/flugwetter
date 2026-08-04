# Makefile for flugwetter project
# Uses podman for container operations

# Image name and tag
IMAGE_NAME = quay.io/slintes/flugwetter

# Deployment target. The restart script lives on the server, not here: it pulls the image,
# replays /var/server/flugwetter.yaml as a podman pod and restarts the nginx proxy in front
# of it. It refers to that yaml by a relative path, so it has to run from /var/server.
SSH_HOST = web
SERVER_DIR = /var/server
RESTART_SCRIPT = restartFlugwetter.sh

# Default target: build and run
.PHONY: all
all: build run

# Run both test suites. The frontend tests are stdlib `node --test` over the modules that
# have no Chart.js or DOM dependency; the glob is quoted so node expands it, not the shell.
.PHONY: test
test:
	gofmt -l main.go internal/server internal/web
	go vet ./...
	go test ./...
	cd internal/web/frontend && node --test 'js/*.test.js'

# Run locally with the frontend served from disk, so CSS and JS edits show up on reload
# without rebuilding the binary. Without this the embedded copy is served.
.PHONY: dev
dev:
	FLUGWETTER_DEV=1 go run .

# Build the container image
.PHONY: build
build:
	podman build -t $(IMAGE_NAME) .

# Run the container. OPENAIP_API_KEY is passed through when set in the environment;
# without it the map picker falls back to the OpenStreetMap base layer alone.
.PHONY: run
run:
	podman run -p 8080:8080 -e OPENAIP_API_KEY $(IMAGE_NAME)

# Push the image to registry
.PHONY: push
push:
	podman push $(IMAGE_NAME)

# Restart the deployment on the server. Only useful after `push` -- the script pulls
# whatever is currently tagged latest in the registry, so restarting without pushing first
# just redeploys the image that is already running.
# -t allocates a tty for password prompts
.PHONY: restart
restart:
	ssh -t $(SSH_HOST) 'cd $(SERVER_DIR) && sudo ./$(RESTART_SCRIPT)'

# Full deployment: build, push, restart. Serial by design -- restarting before the push
# finishes would redeploy the old image.
.PHONY: deploy
deploy: build push restart

# Help target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  all     - Build image and run container (default)"
	@echo "  test    - Run the Go and frontend test suites"
	@echo "  dev     - Run locally with the frontend served from disk"
	@echo "  build   - Build container image"
	@echo "  run     - Run container"
	@echo "  push    - Push image to registry"
	@echo "  restart - Restart the deployment on $(SSH_HOST)"
	@echo "  deploy  - Build, push and restart (full deployment)"
	@echo "  help    - Show this help message"
