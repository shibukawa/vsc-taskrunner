FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/runtask ./cmd/runtask

FROM alpine:3.21

RUN adduser -D -u 10001 appuser

COPY --from=builder /out/runtask /usr/local/bin/runtask

USER appuser

ENTRYPOINT ["/usr/local/bin/runtask"]
CMD ["--help"]