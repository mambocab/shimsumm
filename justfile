build:
    go build -o bin/shimsumm ./cmd/shimsumm

test:
    docker build -t shimsumm-test .
    docker run --rm shimsumm-test

release:
    #!/usr/bin/env sh
    export GITHUB_TOKEN=$(gh auth token)
    goreleaser release --clean --skip=archive

lint:
    shellcheck cmd/shimsumm/smsm_wrap.sh
    go vet ./...

clean:
    rm -rf bin/
