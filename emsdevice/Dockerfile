FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/emsdevice ./cmd/emsdevice

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/emsdevice /usr/local/bin/emsdevice
EXPOSE 1502
USER nobody
ENTRYPOINT ["/usr/local/bin/emsdevice"]
