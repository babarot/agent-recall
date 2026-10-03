.DEFAULT_GOAL := build

.PHONY: build install ui test clean demo

# The web UI is built with npm and embedded into the binary (build tag
# embedui). A plain `go build ./cmd/recall` works too; it leaves the UI out.
build: ui
	go build -tags embedui -o recall ./cmd/recall

install: build
	install -m 755 -v recall $(or $(RECALL_INSTALL_DIR),$(HOME)/.local/bin)/recall

ui:
	cd ui && npm ci && npm run build
	rm -rf internal/webui/dist
	cp -R ui/dist internal/webui/dist

test:
	go vet ./...
	go test ./...
	cd ui && npm test

clean:
	rm -rf recall ui/dist internal/webui/dist

# demo/demo.gif, recorded with VHS from a demo archive (see demo/README.md).
demo:
	go build -o demo/.out/bin/recall ./cmd/recall
	go run ./demo/gen
	vhs demo/demo.tape
