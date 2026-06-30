# Dev image. Build context is apps/api. go run, synced by `docker compose watch`.
FROM golang:1.26-alpine
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
EXPOSE 8080
CMD ["go", "run", "./cmd/api"]
