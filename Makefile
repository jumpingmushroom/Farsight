# Farsight build helpers. Go builds without a web build; `make farsight-ui`
# builds the SvelteKit UI (the `web` target, a prerequisite so a parallel
# `make -j e2e` can't embed a stale build) and embeds it with -tags webui.

.PHONY: web farsight-ui test-web e2e

web:
	cd web && npm ci && npm run build

farsight-ui: web
	go build -tags webui -o farsight ./cmd/farsight

test-web:
	cd web && npm run check && npm test

# Playwright e2e against a seeded farsight (web/tests/e2e/global-setup.ts).
# The UI is built and embedded first; the suite uses that binary via
# FARSIGHT_BIN. Locally, hack/playwright-libs.sh supplies Chromium's system
# libraries and a fontconfig file; the env vars are only set when its
# library directory exists (CI uses `npx playwright install --with-deps`).
e2e: web farsight-ui
	cd web && libs="$$(../hack/playwright-libs.sh --print 2>/dev/null | cut -d: -f1)"; \
	if [ -n "$$libs" ] && [ -d "$$libs" ]; then \
		export LD_LIBRARY_PATH="$$(../hack/playwright-libs.sh --print)" \
			FONTCONFIG_FILE="$$(../hack/playwright-libs.sh --fontconfig)"; \
	fi; \
	FARSIGHT_BIN="$(CURDIR)/farsight" npx playwright test
