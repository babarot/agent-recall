.DEFAULT_GOAL := build

.PHONY: build install ui-build ui-embed clean test go-ui go-build go-test parity

build: ui-build ui-embed
	deno task compile

install: build
	install -m 755 -v agent-recall ~/.claude/agent-recall

ui-build:
	cd ui && npm run build

ui-embed:
	deno run --allow-read --allow-write scripts/embed_ui.ts

# Go port. go-build embeds the web UI; plain `go build` leaves it out.
go-ui: ui-build
	rm -rf internal/webui/dist
	cp -R ui/dist internal/webui/dist

go-build: go-ui
	go build -tags embedui -o recall-go ./cmd/recall

go-test:
	go vet ./...
	go test ./...

# Compare the Go port with the TypeScript version on a snapshot of the
# local archive (read-only for ~/.claude).
parity: ui-build
	scripts/parity/search.sh
	scripts/parity/cli.sh
	scripts/parity/import.sh
	scripts/parity/mcp.py
	scripts/parity/web.sh

clean:
	rm -rf agent-recall recall-go dist/ ui/dist/ src/ui_assets.ts coverage/ internal/webui/dist/

test:
	deno task test
	cd ui && npm test
