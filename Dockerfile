# Multi-stage build for smaller final image
FROM golang:1.24-alpine AS builder

WORKDIR /src

# Dependencies first, so a source-only change reuses this layer. The module has no
# requirements at all, but keeping the step means adding one later does not silently
# invalidate the whole build cache.
COPY go.mod ./
RUN go mod download

# The frontend is compiled into the binary by go:embed, so it has to be present at build
# time -- this is not just a runtime asset copy.
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /flugwetter .

# Final stage - minimal runtime image
FROM alpine:latest

# ca-certificates for HTTPS to Open-Meteo, sunrise-sunset.org and openAIP; tzdata because
# the forecast is processed against real dates.
RUN apk --no-cache add ca-certificates tzdata

RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

WORKDIR /app

# One artifact: the frontend is inside the binary, so there is nothing else to copy and no
# working directory the program has to be started from.
COPY --from=builder --chown=appuser:appgroup /flugwetter ./flugwetter

USER appuser

EXPOSE 8080

CMD ["./flugwetter"]
