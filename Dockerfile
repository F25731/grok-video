FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/grok-video-wrapper ./cmd/server

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/grok-video-wrapper /app/grok-video-wrapper
EXPOSE 8080
CMD ["/app/grok-video-wrapper"]
