# syntax=docker/dockerfile:1

FROM golang:1.24 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Low-memory VPS friendly build: reduce parallelism to avoid OOM.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -p 1 -trimpath -ldflags="-s -w" -o /out/app ./cmd/app

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app

COPY --from=build /out/app /app/app

# Persist these via volumes in docker-compose.
RUN mkdir -p /app/data /app/storage/books

EXPOSE 8080
ENV HTTP_ADDR=:8080

CMD ["/app/app"]
