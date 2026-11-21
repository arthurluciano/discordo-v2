# Build context: the repository root (the server embeds the web app's build).
FROM node:22-alpine AS web
WORKDIR /web
COPY apps/web/package.json apps/web/package-lock.json ./
RUN npm ci
COPY apps/web/ ./
RUN npm run build

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY apps/server/go.mod apps/server/go.sum ./
RUN go mod download
COPY apps/server/ ./
COPY --from=web /web/dist/ ./internal/web/dist/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /discordo ./cmd/discordo

# Static binary on a minimal base, running as an unprivileged user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /discordo /discordo
EXPOSE 8080
EXPOSE 7882/udp
ENTRYPOINT ["/discordo"]
