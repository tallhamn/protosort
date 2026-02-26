.PHONY: build test test-verbose vet lint clean install coverage

build:
	go build -o protosort .

test:
	go test -count=1 ./...

test-verbose:
	go test -v -count=1 ./...

vet:
	go vet ./...

lint: vet
	@gofmt -l . | grep -v vendor | tee /dev/stderr | (! read)

clean:
	rm -f protosort
	rm -f coverage.out coverage.html

install:
	go install .

coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
