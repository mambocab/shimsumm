FROM golang:1.26-bookworm AS builder
WORKDIR /app
COPY go.mod go.sum ./
COPY cmd/ ./cmd/
RUN go build -o bin/shimsumm ./cmd/shimsumm

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    bats curl ca-certificates bash zsh fish shellcheck \
    && rm -rf /var/lib/apt/lists/*
RUN mkdir -p /opt/bats && \
    curl -sL https://github.com/bats-core/bats-support/archive/v0.3.0.tar.gz \
      | tar xz -C /opt/bats && \
    mv /opt/bats/bats-support-0.3.0 /opt/bats/support && \
    curl -sL https://github.com/bats-core/bats-assert/archive/v2.1.0.tar.gz \
      | tar xz -C /opt/bats && \
    mv /opt/bats/bats-assert-2.1.0 /opt/bats/assert
WORKDIR /app
COPY --from=builder /app/bin/shimsumm /app/bin/shimsumm
COPY cmd/shimsumm/smsm_wrap.sh /app/cmd/shimsumm/smsm_wrap.sh
COPY tests/ /app/tests/
CMD ["bats", "/app/tests/"]
