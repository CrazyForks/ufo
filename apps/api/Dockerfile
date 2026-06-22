# Dev image. Build context is the repo ROOT. Migrations + spec are embedded in
# the binary, so only apps/api is needed. go run; synced by `docker compose watch`.
FROM golang:1.26-alpine
WORKDIR /src
COPY apps/api/go.mod apps/api/go.sum ./apps/api/
RUN cd apps/api && go mod download
COPY apps/api ./apps/api
EXPOSE 8080
WORKDIR /src/apps/api
CMD ["go", "run", "./cmd/api"]
