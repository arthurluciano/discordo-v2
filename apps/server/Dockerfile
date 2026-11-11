# Build context: apps/server
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /discordo ./cmd/discordo

# Static binary on a minimal base, running as an unprivileged user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /discordo /discordo
EXPOSE 8080
EXPOSE 7882/udp
ENTRYPOINT ["/discordo"]
