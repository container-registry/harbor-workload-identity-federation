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
    -o /harbor-credential-provider ./cmd/harbor-credential-provider/
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w -X main.version=${VERSION}" \
    -o /harbor-credential-provider-installer ./cmd/harbor-credential-provider-installer/

FROM alpine:3.21

RUN apk add --no-cache ca-certificates util-linux

COPY --from=builder /harbor-credential-provider /usr/local/bin/harbor-credential-provider
COPY --from=builder /harbor-credential-provider-installer /usr/local/bin/harbor-credential-provider-installer
COPY scripts/install-credential-provider.sh /usr/local/bin/install-credential-provider.sh
RUN chmod 0755 /usr/local/bin/install-credential-provider.sh

ENTRYPOINT ["/usr/local/bin/harbor-credential-provider"]
