.DEFAULT_GOAL := build

.PHONY: build install ui-build ui-embed clean test

build: ui-build ui-embed
	deno task compile

install: build
	install -m 755 -v agent-recall ~/.claude/agent-recall

ui-build:
	cd ui && npm run build

ui-embed:
	deno run --allow-read --allow-write scripts/embed_ui.ts

clean:
	rm -rf agent-recall dist/ ui/dist/ src/ui_assets.ts coverage/

test:
	deno task test
	cd ui && npm test
