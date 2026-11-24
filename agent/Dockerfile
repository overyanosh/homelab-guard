FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /build

COPY backend/go.mod ./
COPY backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY agent/ ./agent/

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /homelab-guard-agent ./agent/cmd/agent

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /homelab-guard-agent /usr/local/bin/homelab-guard-agent
ENTRYPOINT ["/usr/local/bin/homelab-guard-agent"]
