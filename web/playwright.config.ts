import { defineConfig } from '@playwright/test';

// End-to-end tests against a real, seeded `farsight serve` (see
// tests/e2e/global-setup.ts). Run with `make e2e`, or from web/:
//
//   LD_LIBRARY_PATH="$(../hack/playwright-libs.sh --print)" \
//   FONTCONFIG_FILE="$(../hack/playwright-libs.sh --fontconfig)" npx playwright test
//
// Chromium runs headless, which in Playwright ≥ 1.49 is the
// chromium-headless-shell build (the one hack/playwright-libs.sh installs).
export default defineConfig({
	testDir: './tests/e2e',
	globalSetup: './tests/e2e/global-setup.ts',
	fullyParallel: true,
	// The tests only read server state; a few workers keep the bcrypt
	// unlocks (cost 12, one per test) from queueing behind each other.
	workers: 4,
	// No retries locally, so a flaky test shows up; one on CI.
	retries: process.env.CI ? 1 : 0,
	forbidOnly: !!process.env.CI,
	reporter: 'list',
	use: {
		browserName: 'chromium',
		headless: true,
		screenshot: 'only-on-failure',
		trace: 'off'
	},
	projects: [
		{
			name: 'desktop',
			testMatch: /(desktop|states)\.spec\.ts$/,
			use: { viewport: { width: 1440, height: 900 } }
		},
		{
			name: 'mobile',
			testMatch: /mobile\.spec\.ts$/,
			use: { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true }
		}
	]
});
