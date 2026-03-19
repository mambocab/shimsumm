FROM golang:1.21-bookworm AS builder
WORKDIR /app
COPY go.mod ./
COPY cmd/ ./cmd/
RUN go build -o bin/shimsumm ./cmd/shimsumm

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    bats curl ca-certificates bash zsh fish \
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
COPY tests/ /app/tests/
CMD ["sh", "-c", "set -e; bats /app/tests/shimsumm-wrap.bats /app/tests/shimsumm-test.bats /app/tests/shimsumm.bats; /app/tests/shells/bash-integration.bash; /app/tests/shells/zsh-integration.zsh; /app/tests/shells/fish-integration.fish"]
