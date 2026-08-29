.PHONY: build test lint clean

build:
	go build -trimpath -o ghemails .

test:
	go test ./...
	go test -race ./...

lint:
	gofmt -d .
	go vet ./...

clean:
	go clean
	rm -f ghemails coverage.out
