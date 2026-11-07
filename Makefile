VERSION ?= 0.2.0
.PHONY: test build package
test:
	go test -race ./...
build:
	CGO_ENABLED=1 go build -buildmode=c-shared -o dist/cpa-codex-reset-manager.so .
package:
	PLUGIN_VERSION=$(VERSION) ./package-release.sh dist
