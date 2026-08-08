# syntax=docker/dockerfile:1

# Stage 1: Build TailwindCSS into static/dist/tailwind.min.css.
FROM node:24-alpine AS build-css
WORKDIR /src
COPY package.json package-lock.json tailwind.config.js ./
COPY static/css/tailwind.css ./static/css/
# Tailwind v4 auto-scans the working directory for class usage, so the HTML
# templates must be present for utility classes to be generated.
COPY internal/templates ./internal/templates/
RUN npm ci && npm run build:css

# Stage 2: Compile the statically linked Go binary.
FROM golang:1.25-alpine AS build-go
ARG VERSION
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=build-css /src/static/dist ./static/dist
RUN VERSION=${VERSION:-dev} \
    && CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -o /minitor . \
    && mkdir -p /data

# Stage 3: Minimal runtime image with a non-root user, entrypoint, and su-exec
# (gosu is not packaged on Alpine; su-exec is its drop-in equivalent).
FROM alpine:3.21

# UID 65532 matches the previous distroless nonroot image for continuity of
# host bind-mount ownership.
RUN apk add --no-cache ca-certificates tzdata su-exec \
    && adduser -D -H -u 65532 -s /sbin/nologin minitor

COPY --from=build-go /minitor /minitor
# Pre-create the data directory owned by the app user. This also seeds fresh
# Docker named volumes with the correct ownership on first mount.
COPY --from=build-go --chown=minitor:minitor /data /data
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

EXPOSE 8080
ENTRYPOINT ["/docker-entrypoint.sh"]
