# syntax=docker/dockerfile:1
FROM node:22-bookworm-slim AS frontend
WORKDIR /build
RUN corepack enable && corepack prepare pnpm@10.15.1 --activate
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
COPY frontend/patches/ ./patches/
RUN --mount=type=cache,id=tuck-pnpm,target=/pnpm/store pnpm install --frozen-lockfile --store-dir=/pnpm/store
COPY frontend/ ./
RUN pnpm build

FROM golang:1.26.4-bookworm AS backend
ARG TARGETOS
ARG TARGETARCH
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -trimpath -ldflags='-s -w' -o /tuck ./cmd/tuck

FROM debian:bookworm-slim AS engine
ARG TARGETARCH
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl python3 && rm -rf /var/lib/apt/lists/*
COPY internal/tuck/contract/engine-release.json /manifest.json
RUN python3 - <<'PY'
import hashlib
import json
import os
from pathlib import Path
import subprocess

manifest = json.loads(Path("/manifest.json").read_text())
arch = {"amd64": "x64", "arm64": "arm64"}[os.environ.get("TARGETARCH") or "amd64"]
asset = manifest["targets"]["linux-" + arch]
binary = Path("/engine") / manifest["version"] / "docker-agent"
binary.parent.mkdir(parents=True)
subprocess.run(["curl", "-fL", "--retry", "0", asset["url"], "-o", str(binary)], check=True)
with binary.open("rb") as source:
    if hashlib.file_digest(source, "sha256").hexdigest() != asset["sha256"]:
        raise SystemExit("bundled official artifact SHA256 mismatch")
binary.chmod(0o755)
PY

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates git ripgrep tini && rm -rf /var/lib/apt/lists/*
WORKDIR /opt/tuck
COPY --from=backend /tuck ./tuck
COPY --from=frontend /build/dist ./frontend/dist/
COPY --from=engine /engine/ ./engine/
RUN mkdir -p /data /config && chown 1000:1000 /data /config
USER 1000:1000
ENV HOME=/data
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s CMD ["/opt/tuck/tuck", "health", "--config", "/config/tuck.config.json"]
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/bin/tini","--","/opt/tuck/tuck"]
CMD ["serve","--config","/config/tuck.config.json"]
