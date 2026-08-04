# Makefile for flugwetter project
# Uses podman for container operations

# Image name and tag
IMAGE_NAME = quay.io/slintes/flugwetter

# The commit being built, with -dirty appended when the tree has uncommitted changes.
# Images are tagged with this as well as :latest, so what is running can be identified and
# an older build can be rolled back to by re-tagging it.
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)$(shell git diff --quiet 2>/dev/null || echo -dirty)
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# Stamped into the binary so /api/config can report it.
VERSION_PKG = flugwetter/internal/server
LDFLAGS = -s -w -X $(VERSION_PKG).commit=$(COMMIT) -X $(VERSION_PKG).buildTime=$(BUILD_TIME)

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
	node --test 'internal/web/jstest/*.test.js'

# Install the checked-in git hooks. core.hooksPath points git at .githooks/ rather than
# copying anything into .git/, so an update to a hook takes effect without reinstalling.
.PHONY: hooks
hooks:
	git config core.hooksPath .githooks
	@echo "git hooks installed (unset with: git config --unset core.hooksPath)"

# Run locally with the frontend served from disk, so CSS and JS edits show up on reload
# without rebuilding the binary. Without this the embedded copy is served.
.PHONY: dev
dev:
	FLUGWETTER_DEV=1 go run .

# Build the container image, tagged with the commit as well as latest.
.PHONY: build
build:
	podman build \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		-t $(IMAGE_NAME):$(COMMIT) \
		-t $(IMAGE_NAME):latest \
		.

# Run the container. OPENAIP_API_KEY is passed through when set in the environment;
# without it the map picker falls back to the OpenStreetMap base layer alone.
.PHONY: run
run:
	podman run --rm --name flugwetter -p 8080:8080 -e OPENAIP_API_KEY $(IMAGE_NAME):latest

# Push both tags. The commit tag is what makes a rollback possible: re-tag an older one as
# latest on the server and restart.
.PHONY: push
push:
	podman push $(IMAGE_NAME):$(COMMIT)
	podman push $(IMAGE_NAME):latest

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

# Show what a build would stamp.
.PHONY: version
version:
	@echo "commit:     $(COMMIT)"
	@echo "build time: $(BUILD_TIME)"
	@echo "image:      $(IMAGE_NAME):$(COMMIT)"

# Help target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  all     - Build image and run container (default)"
	@echo "  test    - Run the Go and frontend test suites"
	@echo "  dev     - Run locally with the frontend served from disk"
	@echo "  hooks   - Install the checked-in git hooks"
	@echo "  version - Show the commit that would be built"
	@echo "  build   - Build container image"
	@echo "  run     - Run container"
	@echo "  push    - Push image to registry"
	@echo "  restart - Restart the deployment on $(SSH_HOST)"
	@echo "  deploy  - Build, push and restart (full deployment)"
	@echo "  help    - Show this help message"
