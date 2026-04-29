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
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" \
    -o /credential-provider-harbor-installer ./cmd/credential-provider-harbor-installer/

FROM alpine:3.21

RUN apk add --no-cache ca-certificates util-linux

COPY --from=builder /credential-provider-harbor /usr/local/bin/credential-provider-harbor
COPY --from=builder /credential-provider-harbor-installer /usr/local/bin/credential-provider-harbor-installer
COPY scripts/install-credential-provider.sh /usr/local/bin/install-credential-provider.sh
RUN chmod 0755 /usr/local/bin/install-credential-provider.sh

ENTRYPOINT ["/usr/local/bin/credential-provider-harbor"]
