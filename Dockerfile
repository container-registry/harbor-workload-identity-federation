FROM golang:1.24-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w -X main.version=${VERSION}" \
    -o /credential-provider-harbor ./cmd/credential-provider-harbor/

FROM alpine:3.21

COPY --from=builder /credential-provider-harbor /credential-provider-harbor

ENTRYPOINT ["/credential-provider-harbor"]
