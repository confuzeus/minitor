# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build-css
RUN apk add --no-cache npm
WORKDIR /src
COPY package.json tailwind.config.js ./
COPY static ./static
RUN npm install && npm run build:css

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
COPY --from=build-css /src/static/dist ./static/dist
RUN CGO_ENABLED=0 go build -o /minitor .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /minitor /minitor
EXPOSE 8080
ENTRYPOINT ["/minitor"]
