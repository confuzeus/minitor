set shell := ["bash", "-uc"]

default:
    @just --list

# Run the server in dev mode
dev:
    go run . -data-dir ./data

# Build CSS and compile the binary
build:
    npm run build:css
    go build -o minitor .

# Run all Go tests
test:
    go test ./...

# Rebuild static CSS
css:
    npm run build:css
