FROM node:24-trixie-slim AS ui-builder

WORKDIR /ui

COPY ui/package*.json ./
RUN npm ci

COPY ui/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26 AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=ui-builder /internal/web/static ./internal/web/static

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/runtask ./cmd/runtask

FROM node:24-trixie-slim AS web-runtime

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git openssh-client \
    && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /var/lib/runtask/repos \
    /var/lib/runtask/home \
    /var/lib/runtask/home/.npm \
    && chown -R 10001:10001 /var/lib/runtask

ENV HOME=/var/lib/runtask/home
ENV NPM_CONFIG_CACHE=/var/lib/runtask/home/.npm

COPY --from=builder /out/runtask /usr/local/bin/runtask

USER 10001:10001

ENTRYPOINT ["/usr/local/bin/runtask"]
CMD ["--help"]
