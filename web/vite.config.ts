import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vitest/config';

// The dev server proxies the API and tiles to a local `farsight serve`.
const backend = 'http://127.0.0.1:8080';

export default defineConfig({
  plugins: [sveltekit()],
  build: {
    // Never inline assets as data: URIs: the CSP's font-src is 'self' only,
    // and small @fontsource subsets would otherwise be inlined and blocked.
    assetsInlineLimit: 0
  },
  server: {
    proxy: {
      '/api': backend,
      '/tiles': backend
    }
  },
  test: {
    include: ['src/**/*.test.ts'],
    environment: 'node',
    env: {
      TZ: 'UTC'
    }
  }
});
