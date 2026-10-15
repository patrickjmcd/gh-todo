BINARY := gh-todo
PREFIX ?= $(HOME)/.local/bin

.PHONY: build install test clean

build:
	go build -o $(BINARY) .

install: build
	mkdir -p $(PREFIX)
	install -m 0755 $(BINARY) $(PREFIX)/$(BINARY)
	@echo "installed $(PREFIX)/$(BINARY)"

test:
	go test ./...

clean:
	rm -f $(BINARY)
