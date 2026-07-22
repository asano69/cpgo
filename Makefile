BINARY := cpgo

.PHONY: build install uninstall fmt vet test clean

build:
	go build -o $(BINARY) .

install:
	go install .

uninstall:
	rm -f $(shell go env GOPATH)/bin/$(BINARY)

fmt:
	gofmt -l .

vet:
	go vet ./...

test: fmt vet
	go build -o /dev/null .
	go test ./...

clean:
	rm -f $(BINARY)
