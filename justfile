build:
    go build -o bin/shimsumm ./cmd/shimsumm

test:
    go test ./...
    docker build -t shimsumm-test .
    docker run --rm shimsumm-test sh -c "bats /app/tests/ && /app/tests/shells/bash-integration.bash && /app/tests/shells/zsh-integration.zsh && /app/tests/shells/fish-integration.fish"

release:
    #!/usr/bin/env sh
    export GITHUB_TOKEN=$(gh auth token)
    goreleaser release --clean --skip=archive

lint:
    shellcheck cmd/shimsumm/smsm_wrap.sh
    go vet ./...

clean:
    rm -rf bin/
