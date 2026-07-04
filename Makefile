BINARY := shadow-mcp
CMD := ./cmd/shadow-mcp
DIST := dist

.PHONY: build test lint clean cross

build:
	go build -o bin/$(BINARY) $(CMD)

test:
	go test ./...

lint:
	go vet ./...

clean:
	rm -rf bin $(DIST)

# Cross-compile static binaries for common platforms.
cross:
	mkdir -p $(DIST)
	GOOS=linux   GOARCH=amd64 go build -o $(DIST)/$(BINARY)-linux-amd64     $(CMD)
	GOOS=linux   GOARCH=arm64 go build -o $(DIST)/$(BINARY)-linux-arm64     $(CMD)
	GOOS=darwin  GOARCH=amd64 go build -o $(DIST)/$(BINARY)-darwin-amd64    $(CMD)
	GOOS=darwin  GOARCH=arm64 go build -o $(DIST)/$(BINARY)-darwin-arm64    $(CMD)
	GOOS=windows GOARCH=amd64 go build -o $(DIST)/$(BINARY)-windows-amd64.exe $(CMD)
