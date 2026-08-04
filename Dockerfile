# Builder. Pinned to a minor version rather than :latest so a rebuild uses the toolchain
# this was tested against.
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Dependencies first, so a source-only change reuses this layer. The module has no
# requirements at all, but keeping the step means adding one later does not silently
# invalidate the whole build cache.
COPY go.mod ./
RUN go mod download

# The frontend is compiled into the binary by go:embed, so it must be present at build
# time -- this is not a runtime asset copy.
COPY . .

# Stamped by the Makefile so /api/config can report what is running.
ARG COMMIT=unknown
ARG BUILD_TIME=

# CGO_ENABLED=0 already produces a static binary, which is what scratch needs. The flags
# that used to be here (-a, -installsuffix cgo, -extldflags "-static") did nothing except
# force a full rebuild of the standard library on every build.
#
#   -trimpath  keeps build paths out of the binary, so it is reproducible
#   -s -w      drops the symbol table and DWARF data
RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w \
            -X flugwetter/internal/server.commit=${COMMIT} \
            -X flugwetter/internal/server.buildTime=${BUILD_TIME}" \
        -o /flugwetter .

# Runtime. scratch, because the binary needs nothing else: the frontend is embedded, the
# timezone database is compiled in, and the only external file is the CA bundle for HTTPS
# to Open-Meteo, sunrise-sunset.org and openAIP.
#
# The cost is that there is no shell to exec into for debugging. `make dev` and `go run .`
# cover that; a container that needs opening up can be rebuilt from the builder stage.
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder --chown=65532:65532 /flugwetter /flugwetter

# Numeric, because scratch has no /etc/passwd to resolve a name against. 65532 is the
# conventional "nonroot" uid.
USER 65532:65532

EXPOSE 8080

# No HEALTHCHECK instruction here on purpose. HEALTHCHECK is a Docker image-schema field --
# the OCI image spec has no equivalent -- so podman drops it unless the image is built with
# --format docker. The image stays OCI; the probe is attached where it belongs instead:
#
#   podman run --health-cmd '/flugwetter -healthcheck' --health-interval=30s ...
#
# or, in a pod manifest, a livenessProbe exec-ing the same command. Either way it runs
# `/flugwetter -healthcheck`, which is the part that has to exist in the image -- scratch
# has no shell, curl or wget for a probe to call.
ENTRYPOINT ["/flugwetter"]
