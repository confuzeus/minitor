set shell := ["bash", "-uc"]

VERSION := `git describe --tags --match 'v*' --abbrev=0 2>/dev/null || echo "dev"`

default:
    @just --list

# Run the server in dev mode
dev:
    go run . -data-dir ./data

# Build CSS and compile the binary
build:
    npm run build:css
    go build -ldflags "-s -w -X main.version={{ VERSION }}" -o minitor .

# Build the binary and Docker image using the git commit hash as the version (for development)
dev-build:
    DEVVERSION="$$(git rev-parse --short HEAD)" && \
    npm run build:css && \
    go build -ldflags "-s -w -X main.version=$$DEVVERSION" -o minitor . && \
    docker build --build-arg VERSION=$$DEVVERSION -t dockershepherd/minitor:$$DEVVERSION .

# Build the Docker image tagged with the current version
docker-build:
    docker build --build-arg VERSION={{ VERSION }} -t dockershepherd/minitor:{{ VERSION }} .

docker-push:
    docker push dockershepherd/minitor:{{ VERSION }}

# Run all Go tests
test:
    go test ./...

# Rebuild static CSS
css:
    npm run build:css
