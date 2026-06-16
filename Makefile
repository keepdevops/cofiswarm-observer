ROLE := observer
.PHONY: build test test-standalone-layout
build:
	go build -o bin/cofiswarm-observer ./cmd/cofiswarm-observer
test: build test-standalone-layout test-gate
test-standalone-layout:
	./test/scripts/assert-layout.sh $(ROLE)
test-gate:
	./test/scripts/test-gate.sh
