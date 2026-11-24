FROM node:22-bookworm-slim AS ui-builder
WORKDIR /ui
COPY frontend/package.json ./
RUN npm install
COPY frontend/ ./
RUN npm run build

FROM golang:1.23-bookworm AS go-builder
WORKDIR /build
COPY backend/go.mod ./
COPY backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=ui-builder /ui/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /homelab-guard ./cmd/server

FROM python:3.12-slim-bookworm
WORKDIR /app

COPY ai-worker/requirements.txt ./ai-worker/
RUN pip install --no-cache-dir -r ./ai-worker/requirements.txt

COPY --from=go-builder /homelab-guard /usr/local/bin/homelab-guard
COPY ai-worker/ ./ai-worker/

RUN mkdir -p /data && useradd -r -u 10001 guard
USER guard

EXPOSE 8443
CMD ["sh", "-c", "uvicorn ai-worker.main:app --host 127.0.0.1 --port 8001 & exec homelab-guard"]
