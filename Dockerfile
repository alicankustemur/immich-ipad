FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /immich-ipad

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /immich-ipad /immich-ipad
EXPOSE 3000
ENTRYPOINT ["/immich-ipad"]
