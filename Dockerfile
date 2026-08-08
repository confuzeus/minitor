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
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=build-css /src/static/dist ./static/dist
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -o /minitor . \
    && mkdir -p /data \
    && chown -R 65532:65532 /data

# Stage 3: Minimal runtime image with ca-certificates, tzdata, and a non-root user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build-go /minitor /minitor
# Pre-create the data directory owned by the non-root user (65532). This also
# seeds fresh Docker named volumes with the correct ownership on first mount.
COPY --from=build-go --chown=65532:65532 /data /data
EXPOSE 8080
ENTRYPOINT ["/minitor"]
