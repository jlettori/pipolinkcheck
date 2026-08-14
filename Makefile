.PHONY: test fmt coverage build clean

test:
	go test -coverprofile='coverage.out' -covermode=atomic ./...

fmt:
	gofumpt -l -w .

coverage:test
	go tool cover -html=coverage.out -o coverage.html && \
	xdg-open coverage.html

build:
	go build .

clean:
	rm -f linkcheck coverage.html coverage.out
