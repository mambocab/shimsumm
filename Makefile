.PHONY: build test clean

build:
	go build -o bin/shimsumm ./cmd/shimsumm

test:
	docker build -t shimsumm-test .
	docker run --rm shimsumm-test

clean:
	rm -rf bin/
