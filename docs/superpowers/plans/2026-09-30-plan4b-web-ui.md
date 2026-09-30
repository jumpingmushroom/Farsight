# Farsight Plan 4b — Web UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Farsight web UI (desktop and mobile) from the Claude Design "World Atlas" against the Plan 4a API, and embed it in the `farsight` binary.

**Architecture:**
- **App shape.** A SvelteKit 2 single-page app (Svelte 5 runes, TypeScript, `adapter-static` with an `index.html` fallback) lives in `web/`. It talks only to the Plan 4a JSON API and tile routes on the same origin.
- **Where logic lives.** Pure logic sits in `web/src/lib/*.ts` and is unit-tested with Vitest:
  - formatting and derived state;
  - marker models, clustering, search;
  - fog mask and coordinates.
  Svelte components stay thin. The map is Leaflet 1.9 with a custom `CRS.Simple` transformation. Markers use a small custom clusterer that matches the design, not `leaflet.markercluster`. The fog is a canvas `GridLayer`.
- **Embedding.** The Go package `web` embeds `web/build` behind the build tag `webui`. Without the tag it serves a "UI not built" page, so `go test ./...` never needs Node. `farsight serve` passes `web.Handler()` as `server.Deps.UI`.

**Tech Stack:** SvelteKit 2, Svelte 5, TypeScript, Vite, Vitest, Leaflet 1.9.4, lucide-svelte, @fontsource (Caprasimo, Figtree), Playwright 1.63. Go 1.27 (`embed`, build tags).

**Spec:** `docs/superpowers/specs/2026-09-29-farsight-atlas-mvp-design.md` ("Web UI", "API contract", "Testing"). **Design:**
- `design/DESIGN-NOTES.md`: an implementation-grade digest of `design/World Atlas.dc.html`, `design/MarkerCard.dc.html` and the Organic design system in `design/_ds/organic-4d151dd9-be22-4870-a515-60b620c2fda0/`.
- Every task names the DESIGN-NOTES sections it builds (for example "§3.3"). Those sections are part of that task's requirements: exact sizes, colours, copy and states.
- Where DESIGN-NOTES marks something **[OUT]** or **[GAP]**, follow its MVP fallback. Where it marks **[AMBIG]**, follow the rulings table below.

## Global Constraints

- The web app lives in `web/`, with package name `farsight-web`, Node ≥ 24, and npm with a committed `package-lock.json`.
- Allowed npm dependencies, and nothing else:
  - runtime: `leaflet@1.9.4`, `lucide-svelte`, `@fontsource/caprasimo`, `@fontsource/figtree`;
  - dev: `@sveltejs/kit`, `@sveltejs/adapter-static`, `@sveltejs/vite-plugin-svelte`, `svelte`, `svelte-check`, `typescript`, `vite`, `vitest`, `@types/leaflet`, `@playwright/test`.
  - No Tailwind and no CSS framework: the Organic `styles.css` is the design system.
- Go rules are unchanged: the only allowed dependencies are `modernc.org/sqlite` and `golang.org/x/crypto`. The `web` package embeds only with `-tags webui`, and `go test ./...` must pass without Node and without a web build.
- At runtime the page makes no request to any other origin. Fonts are self-hosted via @fontsource, so the Google Fonts `@import` in the vendored `styles.css` must be removed. There are no CDNs and no analytics.
- Svelte 5 runes only (`$state`, `$derived`, `$derived.by`, `$effect`, `$props`); no legacy `export let` or stores API in new components. Use `$derived.by(() => …)` for computed values with logic.
- Theme:
  - the root `<html>` element always carries `data-theme="dark"|"light"`;
  - the default is `dark`;
  - the choice persists in `localStorage["farsight.theme"]`, with every access wrapped in try/catch.
- Icons: Lucide at `stroke-width={2.75}` (`lucide-svelte`), except three inline custom SVGs from DESIGN-NOTES §4.3: `portal`, `arch` and legacy `home`. Icon colour comes from `currentColor`.
- Never show player positions or passphrases. A share link `/#s=<id>&k=<passphrase>` unlocks, then immediately replaces the URL with `/#s=<id>` via `history.replaceState`. The passphrase is never stored in `localStorage` or logged.
- API contract (Plan 4a; do not change it here):
  - `GET /api/servers`
  - `GET /api/servers/{id}` (Card)
  - `GET /api/servers/{id}/snapshot`
  - `POST /api/unlock {server, passphrase}` → 204 / 401 / 429
  - `GET /tiles/{id}/{key}/{z}/{x}/{y}.png`
  - All times are RFC 3339 UTC, and absent optional fields are omitted. Locked or unknown servers return 404.
- Clock times are shown as 24-hour `HH:MM` in the viewer's local time zone. Numbers are formatted as in DESIGN-NOTES §6.2: en-US grouping and a U+2212 minus sign.
- Breakpoint: desktop layout at viewport width ≥ 768 px, mobile layout below.
- Commits use `feat(web): …`, `feat(cmd): …`, `test(web): …` or `docs(spec): …`. Each message ends with a blank line and then `Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T`. Use git identity `-c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com"`.
- Never commit `web/node_modules`, `web/build`, `web/.svelte-kit`, `web/test-results` or `web/playwright-report`, and never commit `reference/` or `testdata-golden/`. Test fixtures contain only invented names.
- Headless Chromium in this environment needs `hack/playwright-libs.sh` (Task 1). Run Playwright with `LD_LIBRARY_PATH="$(hack/playwright-libs.sh --print)" FONTCONFIG_FILE="$(hack/playwright-libs.sh --fontconfig)"` (the fontconfig part was added in Task 1: this environment has no system fonts).

## Rulings on design ambiguities (binding; DESIGN-NOTES [AMBIG] items)

| Topic | Ruling |
|---|---|
| Unlock screen (§3.21) | Build the proposed Organic `.dialog`. It is full screen when nothing is unlocked, and also reachable from the switcher's "Add a server…" row. The Server field is a text input for the server id, pre-filled from `#s=`. |
| Status labels (§3.3) | Use the proposed table: Online, Starting, Restarting, Offline, Unknown. |
| Switcher sub-line (§3.5) | For the current server: "Day {day} · {n} of {m} bosses". For others, the status word. The pill reads "1 server" or "N servers". The footer reads "Only servers you’ve unlocked are listed." Close the switcher on an outside click. |
| Recent players (§3.7) | Dedupe by name keeping the newest `until`, exclude names currently online, cap at 5. Hide the label when empty. |
| Activity (§3.8) | Only the newest `world_saved` row is kept, and older ones are dropped. Desktop shows up to 8 rows, mobile 3. `join_code` copy is "New join code {318 742}", `server_ready` "Server is up · version {v}" (or "Server is up"), `server_stopped` "Server stopped", `server_starting`/`server_boot` "Server starting". Empty list: "No server activity yet." |
| Next up (§3.9) | Use the static boss→biome map. Show "Show altar" only when a matching `boss_altar` exists, picking the one nearest the world centre. Hide the box when every boss is defeated. |
| World rules (§3.10) | Use the proposed tile table and preset naming. |
| Map-updated pill (§3.11) | "just now" when N < 1, and "{h} h {m} min ago" when N ≥ 60. Drop the next-save clause and the bar when `saveIntervalSec` is absent. Use "next save any moment" when M ≤ 0. |
| Search (§5.2) | Rank exact, then prefix, then substring; within each rank the kind order is portal, base, tame, sign, altar, trader. Terms include whole names as well as their words. Use the proposed sub-lines. Hints are up to 5 derived from data: the first portal tag, the first named tame, the first base builder, the first boss-altar label, and the first sign text truncated to 20 chars. Omit the hints when there are none. |
| Layers (§3.13) | Show 8 rows (no wards, no ships/carts), count 0 shown. The portals sub-line reads "{n} unpaired", hidden when 0. The counts reflect what is visible after fog filtering. |
| Portal lines (§5.4) | Draw only when both ends are visible after layer and fog filtering. |
| Unpaired copy (§4.2) | Use the proposed three-way copy: one portal with the tag, three or more portals sharing it, and an empty tag. |
| Tame "Near" (§4.2) | The nearest base within 300 m, otherwise omitted. |
| Dungeons (§8) | Kicker "Dungeon", title = `label`. Hidden below Leaflet zoom 3. Altars and traders are shown at every zoom. |
| States (§1.4, §3.22) | The banner replaces the map-updated pill. The progress card is centred in the map area. Stale threshold: 2 × `saveIntervalSec`, or 2 h when it is absent. `refused` and `none` (while a snapshot exists) show the proposed "Can’t draw this world’s map yet" banner. No snapshot shows the pill "Waiting for the first world save…". While charting, markers are hidden and there is no preview; the ETA is estimated from the `done` rate between polls, shown after 2 samples. |
| Mobile (§1.2, §3.18) | Add a close button to the docked MarkerCard and close it on a map tap. The sheet snaps are peek and pulled, dragging between them. The join and server sheets open at full height. Add "Recently online" under the mobile online list. Search results replace the hints and layers in the menu sheet while typing. The theme toggle goes in the menu sheet. |
| Theme (§2.4) | Dark by default, persisted in localStorage. |
| Zoom (§7.3) | Leaflet `minZoom 1`, `maxZoom 6` (native tiles to 5), `zoomSnap 0.25`, `zoomDelta 0.75`, `wheelPxPerZoomLevel 90`. Default view: centre (0, 0) at zoom 1.75 (desktop) or 1.5 (mobile), offset for the panel. `maxBounds` is the world square ±20 %, with `maxBoundsViscosity 0.8`. |
| Refresh cadence | Card every 15 s while the tab is visible, plus immediately on becoming visible. Servers list every 60 s. The snapshot is refetched when `card.world.savedAt` changes. |

---

## File Structure

```
web/package.json, package-lock.json, svelte.config.js, vite.config.ts, tsconfig.json, playwright.config.ts
web/.gitignore                         # node_modules, build, .svelte-kit, test-results, playwright-report
web/embed.go                           # //go:build webui — embeds build/, Handler()
web/noembed.go                         # //go:build !webui — placeholder Handler()
web/handler.go                         # shared: static file handler, SPA fallback, cache + security headers
web/handler_test.go                    # Go tests (placeholder build)
web/src/app.html, app.d.ts
web/src/styles/organic.css             # vendored Organic styles.css (Google @import removed)
web/src/styles/app.css                 # fonts, [data-theme] overrides (§2.2), app-wide atoms, leaflet overrides
web/src/routes/+layout.ts              # export const ssr = false; prerender = false (SPA)
web/src/routes/+layout.svelte          # imports css, theme init
web/src/routes/+page.svelte            # App root: unlock gate, desktop|mobile shell
web/src/lib/types.ts                   # API contract types
web/src/lib/api.ts                     # fetch client
web/src/lib/format.ts                  # §6 formatters
web/src/lib/derive.ts                  # status/stale/charting/next-save/world-rules/recent/activity/next-boss
web/src/lib/share.ts                   # #s=…&k=… parsing
web/src/lib/geo.ts                     # CRS, world↔latlng, scale bar, dir8, distance, zone of point
web/src/lib/fog.ts                     # zone mask builder + tile painter
web/src/lib/markers.ts                 # snapshot → MapMarker[] (kinds, pins, card models, search terms, layers)
web/src/lib/cluster.ts                 # greedy clusterer
web/src/lib/search.ts                  # search ranking + hints
web/src/lib/state.svelte.ts            # App state class (runes): servers, current, card, snapshot, polling, theme, ui flags
web/src/lib/icons/Portal.svelte, Arch.svelte, HomeLegacy.svelte
web/src/lib/components/…               # see tasks
web/src/lib/*.test.ts                  # Vitest unit tests
web/tests/e2e/*.spec.ts                # Playwright
web/tests/fixtures/snapshot.json, events.json   # invented data for e2e seeding
cmd/farsight-seed/main.go              # dev/e2e: posts fixture snapshot+events, writes a fake complete tile set
cmd/farsight/serve.go                  # UI: web.Handler()
hack/playwright-libs.sh                # user-local Chromium system libs (no root)
Makefile                               # web, build-webui, test-web, e2e targets
```

---

### Task 1: Scaffold `web/`, Go embedding, serve wiring, Playwright libs

**Files:**
- Create: `web/package.json`, `web/svelte.config.js`, `web/vite.config.ts`, `web/tsconfig.json`, `web/.gitignore`, `web/src/app.html`, `web/src/app.d.ts`, `web/src/routes/+layout.ts`, `web/src/routes/+layout.svelte`, `web/src/routes/+page.svelte` (placeholder), `web/src/styles/organic.css`, `web/src/styles/app.css`, `web/src/lib/smoke.test.ts`
- Create: `web/handler.go`, `web/embed.go`, `web/noembed.go`, `web/handler_test.go`
- Create: `hack/playwright-libs.sh`, `Makefile`
- Modify: `cmd/farsight/serve.go` (set `UI: web.Handler()`), `.gitignore` (add `/web/node_modules/`, `/web/build/`, `/web/.svelte-kit/`)

**Interfaces:**
- Produces:
  - Go: `package web`, `func Handler() http.Handler`, `var Built bool` (true only with `-tags webui`).
  - npm scripts:
    - `dev`
    - `build`
    - `check` (`svelte-kit sync && svelte-check`)
    - `test` (`vitest run`)
    - `e2e` (`playwright test`)
  - Make targets:
    - `make web`: `npm ci && npm run build` in `web/`
    - `make farsight-ui`: `go build -tags webui -o farsight ./cmd/farsight`
    - `make test-web`: `npm run check && npm test`
    - `make e2e`

- [ ] **Step 1: npm project.** Create `web/package.json`:

```json
{
  "name": "farsight-web",
  "private": true,
  "version": "0.0.1",
  "type": "module",
  "engines": { "node": ">=24" },
  "scripts": {
    "dev": "vite dev",
    "build": "vite build",
    "preview": "vite preview",
    "check": "svelte-kit sync && svelte-check --tsconfig ./tsconfig.json --fail-on-warnings",
    "test": "vitest run",
    "e2e": "playwright test"
  }
}
```

Then install the allowed packages with exact versions resolved by npm (`npm i leaflet@1.9.4 lucide-svelte @fontsource/caprasimo @fontsource/figtree` and `npm i -D @sveltejs/kit @sveltejs/adapter-static @sveltejs/vite-plugin-svelte svelte svelte-check typescript vite vitest @types/leaflet @playwright/test@1.63.0`). Commit `package-lock.json`.

`web/svelte.config.js`:

```js
import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

export default {
  preprocess: vitePreprocess(),
  kit: {
    adapter: adapter({ pages: 'build', assets: 'build', fallback: 'index.html', strict: true }),
    csp: {
      mode: 'hash',
      directives: {
        'default-src': ['self'],
        'script-src': ['self'],
        'style-src': ['self', 'unsafe-inline'],
        'img-src': ['self', 'data:', 'blob:'],
        'font-src': ['self'],
        'connect-src': ['self'],
        'object-src': ['none'],
        'base-uri': ['self'],
        'frame-ancestors': ['none']
      }
    }
  }
};
```

`web/vite.config.ts`: `sveltekit()` plugin. The dev server proxies `/api` and `/tiles` to `http://127.0.0.1:8080`. Vitest runs `src/**/*.test.ts` with `environment: 'node'`.

`web/src/routes/+layout.ts`: `export const ssr = false; export const prerender = false;`

`web/src/app.html` sets `<html lang="en" data-theme="dark">` and a viewport meta with `viewport-fit=cover`. There is no inline script (the CSP forbids it). The stored theme is applied from `AppState` on start (Task 3); the default `dark` attribute avoids a flash for the common case.

- [ ] **Step 2: Styles.**
  - `web/src/styles/organic.css` is a verbatim copy of `design/_ds/organic-4d151dd9-be22-4870-a515-60b620c2fda0/styles.css`, with only the first `@import url('https://fonts.googleapis.com/…')` line removed and a comment saying so.
  - `web/src/styles/app.css` contains:
    - `@import '@fontsource/caprasimo/400.css'; @import '@fontsource/figtree/400.css'; @import '@fontsource/figtree/600.css'; @import '@fontsource/figtree/700.css';`
    - the canvas-level `[data-theme]` block from DESIGN-NOTES §2.2, verbatim, minus the canvas-only `body{background:var(--color-neutral-200)}`;
    - `html,body{height:100%;margin:0;overflow:hidden;background:var(--color-bg);color:var(--color-text);font-family:var(--font-body)}`;
    - `a{color:var(--color-accent-700)}` and `a:hover{color:var(--color-accent-800)}`;
    - the Leaflet reset (`.leaflet-container{background:var(--color-bg);font:inherit}`, with Leaflet's default attribution and zoom controls hidden).
  - `+layout.svelte` imports `leaflet/dist/leaflet.css`, `../styles/organic.css` and `../styles/app.css`.

- [ ] **Step 3: Go handler (test first).** `web/handler_test.go`, in the default build (no tag):
  - `Handler()` on `GET /` returns 200, `text/html`, with a body containing `Farsight UI not built`, and `Cache-Control: no-store`.
  - Security headers are present on every response: `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY`.
  - `newHandler(fstest.MapFS{…})` (the unexported constructor shared with the embed build):
    - `/_app/immutable/x.js` → 200 with `Cache-Control: public, max-age=31536000, immutable` and a JS content type;
    - `/favicon.png` → 200 `no-cache`;
    - `/some/deep/link` (no extension, not a file) → the `index.html` body, with `no-cache`;
    - `/missing.js` (has an extension) → 404;
    - `/../etc/passwd` → never escapes (404 or index);
    - HEAD works;
    - POST → 405.

`web/handler.go`:

```go
package web

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// newHandler serves a SvelteKit static build from fsys: real files as-is,
// extension-less paths fall back to index.html (SPA), immutable assets are
// cached for a year, everything else must revalidate.
func newHandler(fsys fs.FS) http.Handler {
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			h.Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if st, err := fs.Stat(fsys, p); err != nil || st.IsDir() {
			if path.Ext(p) != "" {
				http.NotFound(w, r)
				return
			}
			p = "index.html"
		}
		if strings.HasPrefix(p, "_app/immutable/") {
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			h.Set("Cache-Control", "no-cache")
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + p
		if p == "index.html" {
			r2.URL.Path = "/" // FileServer redirects /index.html → /
		}
		files.ServeHTTP(w, r2)
	})
}
```

`web/embed.go`:

```go
//go:build webui

package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:build
var build embed.FS

// Built reports whether the UI is compiled into this binary.
const Built = true

// Handler serves the embedded web UI.
func Handler() http.Handler {
	sub, err := fs.Sub(build, "build")
	if err != nil {
		panic(err)
	}
	return newHandler(sub)
}
```

`web/noembed.go` (`//go:build !webui`): `const Built = false`. `Handler()` returns a handler that sets the same security headers plus `Cache-Control: no-store`, and writes a small HTML page: "Farsight UI not built. Build with `make web farsight-ui`." (200, `text/html; charset=utf-8`).

- [ ] **Step 4: Wire serve.** In `cmd/farsight/serve.go`, replace `UI: nil, // Plan 4b` with `UI: web.Handler(),`. At startup, log once at Info: `"web ui", "embedded", web.Built`. Run `go test ./... -count=1`; everything stays green.

- [ ] **Step 5: Vitest smoke.** `web/src/lib/smoke.test.ts` asserts that `1+1===2`, which proves the runner is wired (it is removed in Task 2). Run `cd web && npm run check && npm test && npm run build`. Then `go build -tags webui -o /tmp/farsight-ui ./cmd/farsight` must succeed. Start it with a minimal temp config and check that `curl -s localhost:PORT/ | grep -q '<div'` succeeds (the built SvelteKit index), then stop it.

- [ ] **Step 6: Playwright libs script.** `hack/playwright-libs.sh` (bash, `set -euo pipefail`) makes Playwright's headless Chromium runnable without root on Debian trixie:
  - It installs Playwright's `chromium-headless-shell` via `npx playwright install chromium-headless-shell` (run from `web/`).
  - It downloads `Packages.xz` and `Contents-amd64.gz` for `trixie/main` from `http://deb.debian.org/debian` into `~/.cache/farsight-pwlibs/`.
  - It repeatedly runs `ldd` on the headless shell and on every `.so` extracted so far. It maps each missing `soname` to a package via the Contents index, downloads the `.deb` and runs `dpkg-deb -x` into `~/.local/pwlibs`, until nothing is missing.
  - Two packages are hard-coded because their sonames are unversioned: `libnss3` and `libnspr4`.
  - With `--print`, it prints the `LD_LIBRARY_PATH` value (`$HOME/.local/pwlibs/usr/lib/x86_64-linux-gnu:$HOME/.local/pwlibs/lib/x86_64-linux-gnu`) and exits.
  - It is idempotent and quick when everything is present.
  - A small embedded Python or awk step does the soname→package lookup (python3 is available).

  Run it once and confirm that `LD_LIBRARY_PATH="$(hack/playwright-libs.sh --print)" npx playwright --version` works.

- [ ] **Step 7: Commit.** `feat(web): SvelteKit scaffold, Organic styles, embedded UI handler`.

---

### Task 2: API types, client, formatters and derived state

**Files:**
- Create: `web/src/lib/types.ts`, `web/src/lib/api.ts`, `web/src/lib/format.ts`, `web/src/lib/derive.ts`, `web/src/lib/share.ts`
- Test: `web/src/lib/format.test.ts`, `web/src/lib/derive.test.ts`, `web/src/lib/share.test.ts`, `web/src/lib/api.test.ts`
- Delete: `web/src/lib/smoke.test.ts`

**Interfaces:**
- Produces (exact exports):

```ts
// types.ts — mirrors the API contract exactly
export type Status = 'online' | 'starting' | 'restarting' | 'offline' | 'unknown';
export type TileState = 'none' | 'queued' | 'rendering' | 'complete' | 'refused';
export interface ServerSummary { id: string; name: string; status: Status; players: number; maxPlayers: number }
export interface OnlinePlayer { name: string; platform: string; platformId: string; since: string }
export interface RecentSession { name: string; platform: string; platformId: string; since: string; until: string; seconds: number }
export interface Activity { type: string; at: string; name?: string; platform?: string; code?: string; players?: number; seconds?: number; version?: string }
export interface Boss { key: string; name: string; defeated: boolean }
export interface WorldCard { name: string; seedName: string; day: number; bosses: Boss[]; modifiers: Record<string, string>; flags: string[]; exploredPct: number; savedAt: string; readAt: string; saveIntervalSec?: number }
export interface Tiles { state: TileState; done: number; total: number; key?: string }
export interface Card {
  id: string; name: string; crossplay: boolean; address?: string; discordHint?: string; maxPlayers: number;
  status: Status; version?: string; networkVersion?: number; upSince?: string; lastHeartbeat?: string;
  players: number; joinCode?: string; joinCodeAt?: string;
  online: OnlinePlayer[]; recent: RecentSession[]; activity: Activity[]; world?: WorldCard; tiles: Tiles;
}
export interface Marker { id: string; kind: string; x: number; y: number; z: number; label?: string; owner?: string; species?: string; type?: string; pair?: string }
export interface Builder { id: number; name?: string; pieces: number }
export interface Base { id: string; name: string; x: number; z: number; radius: number; pieces: number; builders: Builder[] }
export interface SnapshotView { savedAt: string; exploredZones: [number, number][]; markers: Marker[]; locations: Marker[]; bases: Base[]; players: { id: number; name: string }[] }

// api.ts
export class ApiError extends Error { constructor(public status: number, message: string) }
export function listServers(f?: typeof fetch): Promise<ServerSummary[]>;
export function getCard(id: string, f?: typeof fetch): Promise<Card>;                 // 404 → ApiError(404)
export function getSnapshot(id: string, f?: typeof fetch): Promise<SnapshotView | null>; // 404 → null
export function unlock(server: string, passphrase: string, f?: typeof fetch): Promise<'ok' | 'wrong' | 'limited'>;
export function tileUrl(id: string, key: string): string; // `/tiles/${enc(id)}/${enc(key)}/{z}/{x}/{y}.png`

// format.ts (all take `now: Date` explicitly where time-relative; never read the clock internally)
export function fmtN(n: number): string;                       // −1,234
export function fmtInt(n: number): string;                    // 2,184
export function fmtCode(code: string): string;                 // "318742" → "318 742"
export function fmtClock(iso: string): string;                 // "14:32" local
export function fmtSession(sec: number): string;               // 1h 12m | 38m | 1d 3h
export function fmtUptime(sec: number): string;                // 3d 6h | 5h 12m | 12m
export function fmtActivityTime(iso: string, now: Date): string; // "5 min", "1 h 12 m", "28 Sep"
export function fmtLastSeen(iso: string, now: Date): string;   // "last seen 24 min ago" | "last seen 3 h ago" | "last seen yesterday" | "last seen 28 Sep"
export function fmtMapAge(minutes: number): string;            // "just now" | "12 min ago" | "3 h 41 min ago"
export function fmtDayRef(iso: string, now: Date): string;     // "today 03:12" | "yesterday 03:12" | "28 Sep 03:12"
export function fmtPct(p: number, decimals?: number): string;  // 12.4%
export function fmtKm(m: number): string;                      // "2.5 km"
export function roman(n: number): string;                      // 1..8 → I..VIII

// derive.ts
export interface StatusView { label: string; dot: string; ring: boolean; tone: 'accent' | 'cold' | 'muted' }
export function statusView(s: Status): StatusView;
export function sessionSeconds(since: string, now: Date): number;
export function recentList(card: Card): { name: string; until: string }[];                   // dedupe/exclude/cap 5
export interface ActivityRow { icon: 'log-in' | 'log-out' | 'power' | 'map'; tone: 'ember' | 'neutral'; text: string; at: string }
export function activityRows(card: Card, limit: number): ActivityRow[];
export interface MapPill { age: string; next?: string; progress?: number; savedClock: string }
export function mapPill(world: WorldCard, now: Date): MapPill;
export type MapState =
  | { kind: 'waiting' }                                   // no world yet
  | { kind: 'charting'; pct: number; done: number; total: number; etaMin?: number }
  | { kind: 'refused' }
  | { kind: 'ready'; stale: false }
  | { kind: 'ready'; stale: true; ageText: string; usualMin?: number };
export interface TileSample { at: number; done: number }   // at = epoch ms
export function mapState(card: Card, now: Date, samples?: TileSample[]): MapState;
export function nextEtaMin(samples: TileSample[], total: number): number | undefined;
export function isOfflineLike(s: Status): boolean;          // offline|unknown
export interface JoinCodeView { state: 'live' | 'restarting' | 'offline' | 'none'; code?: string; note: string; status: string }
export function joinCodeView(card: Card, now: Date): JoinCodeView;
export interface RuleTile { key: string; value: string; changed: boolean }
export function worldRules(world: WorldCard): { preset: string; tiles: RuleTile[] };
export const BOSS_BIOME: Record<string, string>;
export function nextBoss(world: WorldCard): { name: string; biome: string } | undefined;
export function bossShort(name: string): string;            // The Elder→Elder, The Queen→Queen
export function switcherSub(s: ServerSummary, card?: Card): string;
export function countText(players: number, max: number, status: Status, sep?: '/' | ' / '): string; // "4/10" | "—/10"

// share.ts
export interface ShareLink { server?: string; key?: string }
export function parseHash(hash: string): ShareLink;   // "#s=abc&k=p%20w" → {server:'abc', key:'p w'}
export function hashFor(server: string): string;      // "#s=abc"
```

- [ ] **Step 1: Write the failing tests.** Use table-driven Vitest cases with a fixed `now = new Date('2026-09-30T12:00:00Z')` and `process.env.TZ = 'UTC'`, set in `vite.config.ts` `test.env`. Clock formatting must be deterministic. Required cases:
  - **format:**
    - `fmtN(-1234.4)` = `"−1,234"` with U+2212, and `fmtN(3050)` = `"3,050"`;
    - `fmtCode("318742")` = `"318 742"`;
    - `fmtSession`: 4320 → `"1h 12m"`, 2280 → `"38m"`, 97200 → `"1d 3h"`;
    - `fmtUptime(3*86400+6*3600)` = `"3d 6h"`;
    - `fmtActivityTime` at now−5 min → `"5 min"`, now−72 min → `"1 h 12 m"`, now−3 days → `"27 Sep"`;
    - `fmtLastSeen` at now−24 min → `"last seen 24 min ago"`, now−3 h → `"last seen 3 h ago"`, the previous calendar day → `"last seen yesterday"`;
    - `fmtMapAge`: 0 → `"just now"`, 12 → `"12 min ago"`, 221 → `"3 h 41 min ago"`;
    - `fmtDayRef` today, yesterday and older;
    - `fmtKm(2450)` = `"2.5 km"`;
    - `roman(8)` = `"VIII"`.
  - **derive:**
    - `statusView` for all five statuses, per the rulings table;
    - `recentList` with duplicates and an online name → deduped, excluded, capped at 5, newest first;
    - `activityRows` keeps only the newest `world_saved`, maps each type to the copy in the rulings table, and applies the limit;
    - `mapPill` with an interval of 1200 s and saved 12 min ago → `{age:'12 min ago', next:'~8 min', progress:0.6}`; saved 25 min ago → `next:'any moment'`, progress clamped to 1; no interval → no `next` and no `progress`;
    - `mapState`:
      - no world → waiting;
      - tiles rendering 389/1365 → charting with pct 28;
      - refused → refused;
      - complete and fresh → ready not stale;
      - complete and 3 h 41 min old with a 1200 s interval → stale with `ageText '3 h 41 min'`, `usualMin 20`;
      - no interval and 2.5 h old → stale without `usualMin`;
      - tiles `none` with a world present → refused (the "can't draw" banner per the ruling);
    - `nextEtaMin` with two samples 60 s apart and a Δdone of 100, 865 remaining → 9;
    - `joinCodeView`:
      - online with a code → live, with the "Issued at today’s 06:00 restart." note;
      - restarting → restarting;
      - offline → offline, "Last seen 03:12";
      - online without a code → restarting look with status "Waiting for code";
      - `crossplay:false` → state `none`;
    - `worldRules`:
      - modifiers `{combat:'hard', deathpenalty:'casual', resources:'more', raids:'less', portals:'casual'}` → the 5 tiles changed with the labels from §3.10, preset "Custom · based on Normal";
      - empty modifiers and flags → all Normal, preset "Normal";
      - `flags:['nomap']` → Map "Disabled", changed;
    - `nextBoss` returns the first undefeated boss with its biome, and undefined when all are defeated;
    - `switcherSub` for the current server with a card → "Day 214 · 4 of 8 bosses", and for another server → "Offline";
    - `countText` offline → "—/10".
  - **share:**
    - `parseHash('#s=mulevikings&k=a%20b')` → both parts;
    - `'#s=x'` → server only;
    - `''` → `{}`;
    - junk → `{}`;
    - `hashFor('a b')` → `'#s=a%20b'`.
  - **api** (inject a fake `fetch`):
    - `listServers` returns `.servers`;
    - `getSnapshot` 404 → null;
    - `getCard` 404 → `ApiError` with status 404;
    - `unlock` 204 → 'ok', 401 → 'wrong', 429 → 'limited', and it sends a JSON body with `credentials:'same-origin'`;
    - `tileUrl('a b','k')` escapes.
- [ ] **Step 2: Run** `npm test` to confirm the tests fail. Then implement each module to satisfy the tests and the DESIGN-NOTES sections cited above: §3.3, §3.5, §3.7, §3.8, §3.9, §3.10, §3.11, §3.19, §3.22, §6.
- [ ] **Step 3: Run** `npm run check && npm test`. Expected: PASS.
- [ ] **Step 4: Commit** `feat(web): API client, formatters and derived view state`.

---

### Task 3: App state, polling, theme, unlock flow and the page shell

**Files:**
- Create: `web/src/lib/state.svelte.ts`, `web/src/lib/components/UnlockDialog.svelte`, `web/src/lib/components/Toast.svelte`, `web/src/lib/components/ThemeToggle.svelte`
- Modify: `web/src/routes/+page.svelte`, `web/src/routes/+layout.svelte`
- Test: `web/src/lib/state.svelte.test.ts`

**Interfaces:**
- Consumes the Task 2 exports.
- Produces:

```ts
// state.svelte.ts
export type Theme = 'dark' | 'light';
export class AppState {
  servers = $state<ServerSummary[]>([]);
  loaded = $state(false);                 // first /api/servers completed
  currentId = $state<string | undefined>(undefined);
  card = $state<Card | undefined>(undefined);
  snapshot = $state<SnapshotView | null | undefined>(undefined); // undefined = not loaded, null = none yet
  theme = $state<Theme>('dark');
  now = $state(new Date());               // ticks every 30 s; components derive from it
  tileSamples: { at: number; done: number }[] = [];
  toast = $state<{ title: string; sub?: string } | undefined>(undefined);
  constructor(deps?: { fetch?: typeof fetch; storage?: Storage | null; location?: Location; history?: History });
  start(): () => void;                    // reads hash, unlocks share link, loads servers, starts timers; returns stop()
  select(id: string): void;               // sets currentId, updates hash, clears card/snapshot, loads immediately
  refresh(): Promise<void>;               // card (+ snapshot when world.savedAt changed)
  refreshServers(): Promise<void>;
  tryUnlock(server: string, passphrase: string): Promise<'ok' | 'wrong' | 'limited'>; // ok → refreshServers + select
  setTheme(t: Theme): void;               // persists (try/catch) and sets document.documentElement.dataset.theme
  showToast(title: string, sub?: string): void; // auto-hide after 2600 ms
}
export const app: AppState; // singleton used by components
```

Behaviour:
- **`start()`:**
  1. Read `theme` from storage.
  2. Parse `location.hash`. With `server` and `key`, call `tryUnlock` and then `history.replaceState(null, '', hashFor(server))`, whatever the result. An unlock failure shows the unlock dialog with the server prefilled and the error message.
  3. Load servers.
  4. The current server is the hash `server` if it's in the list, else the first server.
  5. Start the timers:
     - card every 15 s, skipped while `document.visibilityState === 'hidden'`, and refreshed immediately on `visibilitychange` → visible;
     - servers every 60 s;
     - `now` every 30 s.
- **A card or snapshot 404** (the server got locked, e.g. the cookie key rotated) removes the server from `servers` and selects the next one. With none left, the page shows the full-screen unlock dialog.
- **A network error** keeps the last data. It does not toast every 15 s; it logs to console once per failure streak.
- **`tileSamples`:** each poll appends a sample while tiles are `queued` or `rendering` (keep the last 5) and clears them on `complete`. `mapState` uses the samples for the ETA.

- [ ] **Step 1: Write the failing tests** (`state.svelte.test.ts`). Construct `AppState` with a fake fetch, an in-memory Storage, and fake `location` and `history`. Use Vitest fake timers. Cases:
  - `start()` with `#s=b&k=pw` posts unlock with `{server:'b', passphrase:'pw'}`, calls `replaceState` with `'#s=b'`, and selects `b`;
  - `start()` with no hash selects the first server;
  - a 15 s tick refetches the card;
  - hidden visibility skips the tick;
  - a changed `world.savedAt` triggers a snapshot fetch, and an unchanged one does not;
  - a card 404 drops the server and selects the next;
  - `setTheme('light')` persists, and a storage that throws does not break it;
  - `tryUnlock` 'ok' refreshes servers and selects the id;
  - `showToast` clears after 2600 ms.

  Runes need the Svelte compiler, which is why the test file is named `state.svelte.test.ts` (Vitest with the SvelteKit plugin compiles `.svelte.ts` files). If the runes class can't be tested directly, extract its logic into a plain `Controller` class that takes a `set` callback, and test that. Record this in the report.
- [ ] **Step 2: Implement** `state.svelte.ts`, then `UnlockDialog.svelte` per DESIGN-NOTES §3.21:
  - Organic `.dialog-backdrop` and `.dialog`: the app mark, "Unlock a server", a `.field` "Server" (`.input`, autocomplete off), a `.field` "Passphrase" (`.input type=password`, `autocomplete="current-password"`), and `btn btn-primary btn-block` "Unlock" (disabled while pending).
  - Errors (role=alert): 401 "Wrong passphrase.", 429 "Too many attempts. Try again in a few minutes."
  - `Esc` or clicking the backdrop closes it only when at least one server is unlocked. Submit on Enter.
  - Focus the first empty field on open.
- [ ] **Step 3: Implement** the remaining pieces:
  - `Toast.svelte` per §3.20, `aria-live="polite"`.
  - `ThemeToggle.svelte`: `btn btn-secondary btn-icon`, `aria-label="Toggle theme"`, sun when dark and moon when light, 17 px.
  - `+page.svelte`:
    - calls `app.start()` on mount;
    - until `app.loaded`, renders a blank `--color-bg` screen with a centred "Loading…";
    - with no servers, the full-screen `UnlockDialog` over the empty background;
    - otherwise, the placeholder `<main>` holding `DesktopShell` or `MobileShell`, picked by `matchMedia('(min-width: 768px)')` and reacting to changes. The shell components come in Tasks 6 and 8; until then, render the server name and status as text.
- [ ] **Step 4: Run** `npm run check && npm test && npm run build`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(web): app state, polling, theme and unlock flow`.

---

### Task 4: Leaflet map, tiles, zoom, scale readout and fog

**Files:**
- Create: `web/src/lib/geo.ts`, `web/src/lib/fog.ts`, `web/src/lib/components/AtlasMap.svelte`, `web/src/lib/components/ZoomControls.svelte`, `web/src/lib/components/ScaleReadout.svelte`
- Create (seed tool, specified in Task 9): `cmd/farsight-seed/main.go`, `cmd/farsight-seed/main_test.go`, `web/tests/fixtures/snapshot.json`, `web/tests/fixtures/events.json`
- Test: `web/src/lib/geo.test.ts`, `web/src/lib/fog.test.ts`

**Interfaces:**
- Consumes: `tileUrl`, `Card`, `SnapshotView`.
- Produces:

```ts
// geo.ts
import L from 'leaflet';
export const WORLD_RADIUS = 10500;
export const CRS: L.CRS; // CRS.Simple with transformation (256/21000, 128, -256/21000, 128)
export function toLatLng(x: number, z: number): L.LatLng;          // L.latLng(z, x)
export function fromLatLng(ll: L.LatLng): { x: number; z: number }; // {x: ll.lng, z: ll.lat}
export function metresPerPixel(zoom: number): number;               // 21000 / (256 * 2**zoom)
export function scaleBar(zoom: number): { px: number; label: string }; // §5.7 using metresPerPixel
export function dir8(dx: number, dz: number): string;                // §6.2
export function distance(a: {x:number;z:number}, b: {x:number;z:number}): number;
export function zoneOf(x: number, z: number): [number, number];      // [floor((x+32)/64), floor((z+32)/64)]
export function insideWorld(x: number, z: number): boolean;          // x²+z² ≤ R²
export const WORLD_BOUNDS: L.LatLngBounds;                            // [[-R,-R],[R,R]] in (lat=z,lng=x)
export const MAX_BOUNDS: L.LatLngBounds;                              // ±1.2R

// fog.ts
export const ZN = 330, ZOFF = 165;                 // zone grid: col = zx+ZOFF, row = ZOFF - zz
export function zoneMask(zones: [number, number][]): Uint8Array;      // ZN*ZN, 1 = explored
export function maskWorldRect(): { x0: number; z0: number; size: number }; // world rect covered by the mask: x0=-ZOFF*64-32, z0 (top)=ZOFF*64+32, size=ZN*64
export function createFogLayer(getMask: () => Uint8Array | undefined): L.GridLayer; // canvas GridLayer, pane 'fog'
```

**AtlasMap.svelte:**
- **Props:** `{ card: Card; snapshot: SnapshotView | null | undefined; padLeft: number; dim: number; filter: string; fog: boolean; onready?: (map: L.Map) => void; onclick?: () => void; onmove?: () => void }`.
- **Map creation:** once, with `crs: CRS`, `zoomSnap 0.25`, `zoomDelta 0.75`, `wheelPxPerZoomLevel 90`, `minZoom 1`, `maxZoom 6`, `maxBounds: MAX_BOUNDS`, `maxBoundsViscosity 0.8`, `attributionControl false`, `zoomControl false`.
- **Panes:**
  - `fog`: zIndex 350;
  - `portalLines`: 380;
  - `markers`: Leaflet's default markerPane 600;
  - `popover`: handled in DOM, not a pane.
- **Tile layer:** only when `card.tiles.state === 'complete' && card.tiles.key`, using `L.tileLayer(tileUrl(id, key), { tileSize: 256, minZoom: 0, maxNativeZoom: 5, maxZoom: 6, noWrap: true, bounds: WORLD_BOUNDS, keepBuffer: 2 })`. A key or server change replaces the layer.
- **World disc:** the backdrop behind the tiles is a styled `L.circle([0,0], {radius: R})` in its own pane below the tiles. It uses fill `color-mix(in srgb, var(--color-text) 6%, transparent)` and a stroke of `5px color-mix(text 12%)`, which reproduces the §1.1 world image ring. Draw it with a CSS class; use CSS variables through `className`, not inline colours.
- **Filters:** the tile pane gets `filter` from props (layers off → greyscale; offline → `grayscale(.55) brightness(.8)`; stale → `sepia(.35) brightness(.9)`, per §3.22). `dim` applies `brightness()` to the whole map container for mobile sheets.
- **Default view:** `setView(toLatLng(0,0), 1.75)`, then pan by `-padLeft/2` px horizontally so the world centres in the visible area. Export `resetView()` and `centerOn(x, z, zoom)`, both honouring `padLeft`: the target lands at the centre of the area right of the panel.
- **Cursor:** `grab`, and `grabbing` while dragging, via Leaflet's defaults.

**ZoomControls.svelte** (§3.15): `onzoomin`/`onzoomout`/`onreset` callbacks, with the `mobile` variant from §1.2 frame 1 (three 52 px circles).

**ScaleReadout.svelte** (§3.14): shows the `scaleBar(zoom)` bracket and label, then `X {fmtN} · Z {fmtN}` from the mousemove, rAF-throttled. The third slot shows "World edge" outside the disc, "Unexplored" when fog is on and the zone is not in the mask, and otherwise nothing (no biome data, per [GAP]). The default is "X — · Z —" with "Hover the map". It is hidden on `(pointer: coarse)`.

**Fog layer:** a `L.GridLayer` whose `createTile(coords)` returns a 256×256 canvas. It must:
1. Compute the tile's world rect from `coords` (tile size 256 at zoom `coords.z`, via `map.unproject`).
2. Clip to the world disc (`arc` in tile pixels).
3. Fill `#cfbe9c`, add ±9 grey noise with a seeded PRNG keyed on the tile coords (so redraws are stable), and draw the hatching every 9 screen px at 45° with `rgba(120,98,66,.12)`, 1 px.
4. Set `globalCompositeOperation='destination-out'` and `filter = blur(${0.9*64/metresPerPixel(z)}px)` (capped at 24 px).
5. `drawImage` a cached `ZN×ZN` mask canvas (white = explored) scaled to the mask's world rect projected into tile pixels, with `imageSmoothingEnabled = true`.
6. Redraw everything when the mask changes (`layer.redraw()`).
7. Keep the pane's pointer-events off.

- [ ] **Step 1: Write the failing tests.**
  - **geo:**
    - `CRS.latLngToPoint(toLatLng(-10500, 10500), 0)` ≈ (0,0);
    - `(10500,-10500)` at zoom 0 ≈ (256,256);
    - `(0,0)` at zoom 5 ≈ (4096,4096);
    - `fromLatLng(toLatLng(123,-456))` round-trips;
    - `metresPerPixel(5)` = 2.5634765625;
    - `scaleBar(3)` = `{px: 98, label: '1 km'}` (metresPerPixel(3) = 10.2539; target = 110 × 10.2539 = 1127.9 m; the first nice value ≥ 0.7 × target = 789.5 is 1000; px = round(1000 / 10.2539) = 98);
    - `dir8(0,1)`=north, `dir8(1,0)`=east, `dir8(-1,-1)`=south-west;
    - `zoneOf(31.9, -32)` = `[0, 0]` and `zoneOf(32, -32.1)` = `[1, -1]` (`-0` normalised to `0`);
    - `insideWorld(10500,0)` true, `(10500,1)` false.
  - **fog:**
    - `zoneMask([[0,0],[-164,164],[164,-164]])` sets exactly indices `165*ZN+165`, `1*ZN+1` and `329*ZN+329` (row = ZOFF − zz, col = zx + ZOFF);
    - out-of-range zones are ignored without throwing;
    - `maskWorldRect()` = `{x0:-10592, z0:10592, size:21120}`.
- [ ] **Step 2: Run** the tests to confirm they fail. Implement `geo.ts` and `fog.ts`, then the components. `AtlasMap` cannot be unit-tested in Node; it is covered by Task 9's Playwright tests.
- [ ] **Step 3: Temporary wiring.** Mount `AtlasMap` full-screen in `+page.svelte`'s placeholder shell for the current server, with the ZoomControls and ScaleReadout. Run `npm run check && npm test && npm run build`.
- [ ] **Step 4: Seed tool and fixtures.** Build `cmd/farsight-seed` and the fixtures exactly as specified in Task 9, "Seed tool" (Steps 1–2), so this task and Tasks 5–8 can check their work visually against a real server. Then build with `-tags webui`, start `farsight serve` with a temp config (data under the session scratchpad), run `farsight-seed -fake-tiles` and seed `demo`. Take a Playwright screenshot at 1440×900 showing the tiles, the fog outside the explored disc, and the scale readout. Screenshots are never committed.
- [ ] **Step 5: Commit** `feat(web): Leaflet atlas with world CRS, tiles, zoom, scale readout and fog` and `feat(cmd): farsight-seed dev tool and demo fixtures`.

---

### Task 5: Markers, clustering, portal lines, selection and MarkerCard

**Files:**
- Create: `web/src/lib/markers.ts`, `web/src/lib/cluster.ts`, `web/src/lib/search.ts`, `web/src/lib/icons/Portal.svelte`, `web/src/lib/icons/Arch.svelte`, `web/src/lib/icons/HomeLegacy.svelte`, `web/src/lib/components/MarkerIcon.svelte`, `web/src/lib/components/MarkerLayer.svelte`, `web/src/lib/components/MarkerCard.svelte`
- Test: `web/src/lib/markers.test.ts`, `web/src/lib/cluster.test.ts`, `web/src/lib/search.test.ts`

**Interfaces:**
- Consumes: `SnapshotView`, `Card`, `geo.ts`, `fog.zoneMask`, `format.ts`.
- Produces:

```ts
// markers.ts
export type LayerKey = 'biomes' | 'structures' | 'portals' | 'beds' | 'tombstones' | 'tames' | 'signs' | 'locations';
export const LAYERS: { key: LayerKey; label: string; short: string; icon: IconName; default: boolean }[]; // §3.13 minus vehicles/wards, in that order
export type IconName = 'portal' | 'home' | 'bed' | 'skull' | 'paw-print' | 'signpost' | 'flame' | 'coins' | 'arch' | 'mountain';
export type PinType = 'base' | 'portal' | 'tomb' | 'altar' | 'bed' | 'tame' | 'sign' | 'trader' | 'dungeon';
export interface Pin { size: number; bg: string; border: string; icon: IconName; iconColor: string; iconPx: number; ring?: boolean }
export interface CardModel {
  kicker: string; title: string; icon: IconName; discBg: string;
  badges: { text: string; tone: 'cold' | 'ember' | 'sage' | 'neutral' }[];
  facts: { k: string; v: string }[]; note?: string; partnerId?: string; where: string;
}
export interface MapMarker {
  id: string; type: PinType; layer: LayerKey; x: number; z: number; zone: [number, number];
  title: string; tooltip: string; pin: Pin; card: CardModel; terms: string[]; kindOrder: number;
  label?: string;          // base name label under the disc
  minZoom?: number;        // dungeons: 3
}
export function buildMarkers(snap: SnapshotView, world?: WorldCard): MapMarker[];
export function visibleMarkers(all: MapMarker[], layers: Record<LayerKey, boolean>, mask: Uint8Array | undefined, fog: boolean, zoom: number): MapMarker[];
export function layerCounts(all: MapMarker[], mask: Uint8Array | undefined, fog: boolean): Record<LayerKey, number> & { unpaired: number; pairs: number };
export function portalPairs(visible: MapMarker[]): [MapMarker, MapMarker][]; // both ends visible, each pair once

// cluster.ts
export interface ClusterPoint { id: string; x: number; y: number }
export interface ClusterGroup { ids: string[]; x: number; y: number }
export function cluster(points: ClusterPoint[], radius: number, lockedId?: string): ClusterGroup[];

// search.ts
export interface SearchResult { id: string; title: string; sub: string; pin: Pin; icon: IconName }
export function search(all: MapMarker[], q: string, mask: Uint8Array | undefined, fog: boolean, limit?: number): SearchResult[]; // limit default 8
export function hints(all: MapMarker[]): string[];
```

`cluster.ts`, the exact algorithm from §5.1:

```ts
export function cluster(points: ClusterPoint[], radius: number, lockedId?: string): ClusterGroup[] {
  const groups: (ClusterGroup & { locked: boolean })[] = [];
  const r2 = radius * radius;
  for (const p of points) {
    if (p.id === lockedId) {
      groups.push({ ids: [p.id], x: p.x, y: p.y, locked: true });
      continue;
    }
    let g = groups.find((g) => !g.locked && (g.x - p.x) ** 2 + (g.y - p.y) ** 2 < r2);
    if (!g) {
      groups.push({ ids: [p.id], x: p.x, y: p.y, locked: false });
      continue;
    }
    const n = g.ids.length;
    g.x = (g.x * n + p.x) / (n + 1);
    g.y = (g.y * n + p.y) / (n + 1);
    g.ids.push(p.id);
  }
  return groups.map(({ ids, x, y }) => ({ ids, x, y }));
}
```

**buildMarkers** follows §4.1 (pin styles), §4.2 (kinds, with every MVP adaptation), §8 (backend kind → type and layer) and the rulings:
- Markers are sorted in the order bases, portals, altars, traders, dungeons, tombstones, tames, beds, signs, so clustering and search are stable.
- Base `label` = base title (shown at zoom ≥ 1.5).
- `where` = `X {fmtN(x)} · Z {fmtN(z)}`.
- Portals:
  - the partner fact is `{fmtKm(dist)} {dir8}`;
  - the note is "Leads to the “{tag}” portal {km} {dir}.";
  - use the three-way unpaired copy;
  - the tag fact is `“{tag}”`;
  - an empty tag gives the title "Untagged portal".
- Tame Near uses the nearest base within 300 m.
- Altar `defeated` is joined from `world.bosses` by name (not defeated when `world` is absent).
- Dungeon `type` → icon: `MountainCave02`, `TrollCave02`, `BearCave` and `Hildir_cave` → mountain; everything else → arch.
- Colours are the fixed constants INK `#26231f`, CREAM `#f5ead8`, COLD `#3d7eab`, EMBER `#c67139` and SAGE `#7a8a5e` (theme-independent).

**MarkerLayer.svelte:**
- **Props:** `{ map: L.Map; all: MapMarker[]; layers; mask; fog; selectedId?: string; clustering?: boolean (default true); onselect: (id: string | undefined) => void }`.
- **Rendering:**
  - On `zoomend`, `moveend` and prop changes, compute the visible markers, then `map.latLngToContainerPoint` for each marker. Keep the points within the viewport ±40 px.
  - Cluster with radius 26, locking the selected marker.
  - Render one `L.marker` per group with an `L.divIcon`, in a layer group that is rebuilt each time. Diff by a group key (sorted ids) to avoid flicker.
  - Single pins use `MarkerIcon` HTML (§4.1), including the selected ring. Clusters use the bubble from §5.1, with the tooltip `{n} markers · {up to 3 kickers}`.
- **Interaction:**
  - Clicking a cluster runs `map.setZoomAround(latlng, map.getZoom() + 1.25)`.
  - Clicking a pin calls `onselect(id)`.
  - Base labels are drawn when zoom ≥ 1.5.
- **Portal lines:** `L.polyline`s in the `portalLines` pane, for `portalPairs` when `layers.portals && portalLinks`, styled per §5.4 (via className and CSS).

**MarkerCard.svelte** follows `design/MarkerCard.dc.html` exactly (§3.16):
- **Props:** `{ m: CardModel; onclose?: () => void; onjump?: (id: string) => void }`.
- The badge tones come from §3.16.
- The icons render via `MarkerIcon` (Lucide or custom).

**Popover (desktop):** the page positions the MarkerCard (312 px) per §5.9, following the pin on move and zoom. `Esc` closes it, and so does a map click that doesn't hit a pin. "Jump to partner" runs `centerOn(partner, max(3.5, zoom))` and then selects the partner.

- [ ] **Step 1: Write the failing tests.**
  - **cluster:**
    - two points 10 px apart form one group, whose centroid is their mean;
    - points 30 px apart stay separate;
    - a running centroid: three collinear points at 0, 20 and 40 → the first two merge (centroid 10), and the third at 40 is 30 away, so it stays separate;
    - the locked id never merges, even when coincident;
    - input order is preserved for group creation.
  - **markers**, from a hand-written snapshot fixture with one of each kind: 2 portals sharing "home", 1 portal "copper", 3 portals "hub", 1 portal with an empty tag, a bed with an empty owner, a tombstone, a named tame near a base (200 m), an unnamed Hen, a blank sign, a base with 3 builders (one unnamed), altars for Eikthyr (defeated) and Yagluth, Haldor, SunkenCrypt4 and TrollCave02:
    - each kind maps to the right type, layer and pin (sizes and colours per §4.1);
    - paired portal: the "Paired" badge, facts `Tag “home”` and `Partner {km} {dir}`, and `partnerId`;
    - the copper portal gets the one-portal note, and the hub portals the "3 portals share the tag “hub”…" note;
    - the untagged portal's title is "Untagged portal";
    - a bed with an empty owner shows Owner "Unknown";
    - the Hen's title is "Hen", with no Name fact;
    - the named tame has Near = the base name;
    - the sign's title is "Blank sign";
    - base facts: Builder, Pieces with a comma, and "Also built by" including "Unknown builder";
    - altar badges: "Defeated" (sage) and "Not yet defeated" (neutral);
    - dungeons: kicker "Dungeon", `minZoom` 3, and the mountain icon for TrollCave02;
    - `where` uses U+2212;
    - `visibleMarkers` hides fogged-out zones when fog is on, hides a layer that's off, and hides dungeons below zoom 3;
    - `layerCounts` gives pairs = 1 and unpaired = 5 (copper, 3 hub, untagged);
    - `portalPairs` returns only pairs with both ends visible.
  - **search:**
    - "hom" → the paired home portals first (prefix rank), capped at 8;
    - an exact tame-name match ranks above prefix and substring matches;
    - a builder's full name "halvor's" matches the base title;
    - beds, tombstones and dungeons are never returned;
    - fogged markers are excluded;
    - `hints` returns up to 5 unique, non-empty values in the ruled order.
- [ ] **Step 2: Run** the tests to confirm they fail, then implement `markers.ts`, `cluster.ts`, `search.ts` and the components. Wire `MarkerLayer` and the desktop popover into the placeholder shell. Selection state lives in the page, as `selectedId`.
- [ ] **Step 3: Run** `npm run check && npm test && npm run build`. Expected: PASS.
- [ ] **Step 4: Commit** `feat(web): markers, design clustering, portal lines and marker cards`.

---

### Task 6: Desktop side panel, server card, switcher, world tab and join dialog

**Files:**
- Create in `web/src/lib/components/`:
  - `DesktopShell.svelte`
  - `SidePanel.svelte`
  - `ServerCard.svelte`
  - `ServerSwitcher.svelte`
  - `PlayersTab.svelte`
  - `ActivityList.svelte`
  - `WorldTab.svelte`
  - `BossGrid.svelte`
  - `WorldRules.svelte`
  - `ExploredBar.svelte`
  - `CollapsedPill.svelte`
  - `MapUpdatedPill.svelte`
  - `JoinContent.svelte` (shared body)
  - `JoinDialog.svelte`
  - `AppMark.svelte`
  - `Avatar.svelte`
- Modify: `web/src/routes/+page.svelte` (desktop branch → `DesktopShell`)

**Interfaces:**
- Consumes: `app` (AppState), and the derive and format functions.
- Produces:
  - `DesktopShell` (no props): it composes the map, side panel, pills, overlays and dialogs, and owns the UI flags `panelOpen`, `tab`, `selectedId`, `joinOpen`, `layers` and `portalLinks`.
  - `JoinContent.svelte`: props `{ card: Card; variant: 'desktop' | 'mobile'; oncopy: (title: string, sub: string) => void }`. It's reused by the mobile join sheet in Task 8.
  - `WorldRules.svelte`: props `{ world: WorldCard }`.

Build to these DESIGN-NOTES sections exactly:
- §1.1: layout, z-order and pill positions (`pillL` 376 with the panel open, 190 closed; `readL` 376 or 16).
- §3.1: the app mark.
- §3.2: the panel header, with ThemeToggle and collapse.
- §3.3: the server card, using the status mapping from derive.
- §3.4: the collapsed pill.
- §3.5: the switcher dropdown, `role=listbox`, with keyboard support: ↑/↓/Enter/Esc and a click outside to close. It gains an "Add a server…" row that opens the UnlockDialog, and uses the ruled footer copy.
- §3.6: the tabs "Online · {players}" and "World".
- §3.7: the players list, with no "Profile →".
- §3.8: the activity list, with no "Full timeline →". Rows are not clickable in the MVP.
- §3.9: the World tab: tiles, the boss grid with API names and short names, "Next up" with "Show altar" (runs `centerOn(altar, 3.5)` and selects it), World rules, the explored bar and the footnote. Without a world, it shows "No world save read yet."
- §3.10: world rules.
- §3.11: the map-updated pill, re-deriving from `app.now`.
- §3.19: the join dialog:
  - the platform segmented control, the join-code box driven by `joinCodeView`, the address box (PC only, hidden without an address), the steps, the info box (password line per the ruling, version line, console note) and the world rules;
  - "Copy code" copies the raw digits and toasts "Join code copied" with "318 742", and the address copy toasts "Address copied";
  - `navigator.clipboard.writeText` has a fallback (select the text) on rejection;
  - for a Steam-only server, hide the platform switch and code box and show the address box as primary, with the kicker "Join by address";
  - `Esc` or a backdrop click closes it, and focus is trapped inside.
- §3.22: the offline and nobody-online empty states inside the Players tab. These use the real side panel, not the mini panel.

Accessibility:
- All buttons are real `<button>`s with the aria labels from the design.
- Tabs use `role=tablist`/`tab`/`tabpanel`.
- The dialog has `role=dialog`, `aria-modal=true` and `aria-labelledby`.

- [ ] **Step 1: Implement** the components. Wire `DesktopShell` into `+page.svelte` for widths ≥ 768 px, with the map from Task 4 and the markers from Task 5.
- [ ] **Step 2: Run** `npm run check && npm test && npm run build`. Expected: PASS. There are no new unit tests here, because the logic lives in derive and format, which Task 2 already tests. Components are covered by Task 9's Playwright tests.
- [ ] **Step 3: Visual self-check.** Run `vite dev` against a `farsight serve` holding seeded data, or mock the API with a temporary `vite` middleware that serves `web/tests/fixtures`. Take Playwright screenshots at 1440×900 in both themes (`LD_LIBRARY_PATH="$(hack/playwright-libs.sh --print)"`), and compare them by eye with DESIGN-NOTES §1.1 and §3. Fix mismatches. Screenshots go to the scratch dir and are never committed.
- [ ] **Step 4: Commit** `feat(web): desktop side panel, server card, world tab and join dialog`.

---

### Task 7: Desktop overlays — search, layers, banners and charting

**Files:**
- Create in `web/src/lib/components/`: `SearchBox.svelte`, `SearchResults.svelte`, `LayersButton.svelte`, `LayersPanel.svelte`, `Legend.svelte`, `StateBanner.svelte`, `ChartingCard.svelte`, `WaitingPill.svelte`
- Modify: `DesktopShell.svelte`

**Interfaces:**
- Consumes: `search`, `hints`, `LAYERS`, `layerCounts`, `mapState`, and `nextEtaMin` via `app.tileSamples`.
- Produces:
  - `SearchResults.svelte`: props `{ results: SearchResult[]; hints: string[]; q: string; onpick: (id) => void; onhint: (h) => void }`. It's reused by mobile.
  - `LayersPanel.svelte`: props `{ layers; portalLinks; counts; variant: 'desktop' | 'mobile'; ontoggle; onlinks }`. It's reused by mobile.

Build to:
- §1.1 items 6, 8 and 9: the top-right cluster, the readout and the zoom positions.
- §3.12: search.
  - Focus opens the results and closes the layers panel.
  - Blur closes the results after 120 ms.
  - Picking a result uses `mousedown` + `preventDefault`.
  - ↑/↓/Enter move through and pick results; Esc clears.
  - Picking runs `centerOn(m, 4.25)` and selects the marker.
  - The empty-match copy is "Nothing matches “{q}” in the last save."
  - With no snapshot, the input is disabled with the placeholder "Waiting for the first save…".
- §3.13: layers.
  - The count badge counts the enabled main layers.
  - The rows, the "Show connections" sub-row with its disabled state, and the legend use the **tile palette** biome colours from DESIGN-NOTES §3.13 plus Unexplored `#cfbe9c`.
  - "Biomes & terrain" off applies the greyscale map filter `grayscale(1) contrast(.85) brightness(1.08)`.
- §3.22 and the rulings: states.
  - `StateBanner` shows the offline and stale variants plus the "Can’t draw this world’s map yet" variant, replacing the map-updated pill.
  - `ChartingCard` shows the design copy with `{done} of {total}` (total from the API) and "~{n} min left" only when the ETA is known. It is centred in the map area and markers are hidden.
  - `WaitingPill` reads "Waiting for the first world save…".
  - Map filters and pin opacity follow §3.22: offline pins at .75, stale at .85, set through a CSS class on the marker pane.
- The layers and search menus close when: focusing search, opening join, selecting a marker, or clicking the map (§5.3).

- [ ] **Step 1: Implement** the components and wire them into `DesktopShell`.
- [ ] **Step 2: Run** `npm run check && npm test && npm run build`. Expected: PASS.
- [ ] **Step 3: Visual self-check** as in Task 6, for:
  - search open with results;
  - the layers panel open;
  - each of the four states, using a temporary card override (a mocked API response via Playwright `page.route`).
- [ ] **Step 4: Commit** `feat(web): search, layers, state banners and charting progress`.

---

### Task 8: Mobile layout

**Files:**
- Create in `web/src/lib/components/`: `MobileShell.svelte`, `MobileTopBar.svelte`, `BottomSheet.svelte`, `PeekSheet.svelte`, `MenuSheet.svelte`, `JoinSheet.svelte`, `ServerSheet.svelte`, `MobileMarkerCard.svelte`
- Modify: `web/src/routes/+page.svelte` (the < 768 px branch → `MobileShell`)

**Interfaces:**
- Consumes everything above. It reuses `JoinContent`, `SearchResults`, `LayersPanel` (the `mobile` variant renders the 2-column tile grid from §3.18), `MarkerCard`, `ZoomControls` (mobile variant), `ActivityList` (limit 3) and `PlayersTab` pieces.
- Produces: `BottomSheet.svelte` with props `{ snap: 'peek' | 'pulled' | 'full' | 'auto'; onsnap?: (s) => void; surface: 'glass' | 'surface'; children }`:
  - pointer-drag on the handle and header, between peek and pulled, choosing the nearest snap on release with a 150 ms ease;
  - `touch-action: none` on the handle;
  - respects `env(safe-area-inset-bottom)`.

Build to:
- §1.2: all four frames.
- §1.6 and §1.7: the mobile join and server sheets (full height, from `top: calc(env(safe-area-inset-top) + 8px)`).
- §3.17: the top bar at `top: calc(env(safe-area-inset-top) + 8px)`, with the sub-line "Map {N} min ago · next ~{M} min", or the state text when not ready.
- §3.18: the peek, pulled and menu sheets, plus the rulings:
  - the menu sheet holds the search field (16 px font), hints or results, Layers, portal connections and a theme row "Theme · Dark/Light" using ThemeToggle;
  - search results replace the hints and layers while typing, and picking one closes the menu and selects the marker;
  - "Recently online" appears under the pulled online list.
- Map dimming: `brightness(.7)` pulled, `.55` for the menu, join and server sheets.
- The docked `MobileMarkerCard` sits at `left:12px; right:12px; bottom: calc(28px + env(safe-area-inset-bottom))` with a close button. A tap on the map closes it. Selecting a marker centres it at y ≈ 300 px.
- No readout, and no desktop pills.
- Mobile states:
  - offline, stale and can't-draw banners become a compact banner under the top bar (the same copy);
  - charting shows `ChartingCard` centred.

- [ ] **Step 1: Implement** the components and wire `MobileShell`.
- [ ] **Step 2: Run** `npm run check && npm test && npm run build`. Expected: PASS.
- [ ] **Step 3: Visual self-check** at 390×844 for the peek, pulled, menu, marker-tapped, join and server-switcher views, in dark and light, compared against DESIGN-NOTES §1.2, §1.6 and §1.7.
- [ ] **Step 4: Commit** `feat(web): mobile top bar, bottom sheets, menu and join sheet`.

---

### Task 8b: Seamless fog (added during execution)

Reviews of Tasks 5 and 8 measured 1 px seams along fog tile edges at fractional zoom. They sit in the soft fog band, at both DPR 1 and DPR 2, and a tiled GridLayer can't avoid them. This task replaces the fog `GridLayer` with a single viewport-sized canvas layer:
- DPR-backed;
- a parchment/hatch pattern anchored to world pixels;
- blurred mask punch-out for the visible rect only;
- redrawn on move/zoom end and transformed during zoom animation.

`fog.ts` keeps its exported mask helpers and `createFogLayer` call shape. The full brief is in the SDD workspace as `task-8b-brief.md`. Acceptance: seam edge scores at control level at zooms 1.5 / 1.75 / 3.25 / 3.5 / 4.25, DPR 1 and 2; boundary alignment unchanged; redraw under ~20 ms at 1440×900@2x. Commit `fix(web): seamless single-canvas fog layer`.

---

### Task 9: Playwright end-to-end tests and spec amendment

The "Seed tool" part of this task (the interface, Step 1 and Step 2) is **built in Task 4**. It is specified here, next to the e2e tests that depend on it. In Task 9, only extend it if the e2e tests need something more.

**Files:**
- Already created in Task 4: `cmd/farsight-seed/main.go`, `cmd/farsight-seed/main_test.go`, `web/tests/fixtures/snapshot.json`, `web/tests/fixtures/events.json`
- Create: `web/playwright.config.ts`, `web/tests/e2e/global-setup.ts`, `web/tests/e2e/desktop.spec.ts`, `web/tests/e2e/mobile.spec.ts`, `web/tests/e2e/states.spec.ts`
- Modify: `Makefile` (the `e2e` target), `docs/superpowers/specs/2026-09-29-farsight-atlas-mvp-design.md`

**Interfaces:**
- Consumes: `internal/ingest.Client` (gzip + bearer POST), `internal/tiles.SetDir`, `tiles.RenderVersion`, `tiles.TileSize`, `tiles.MaxZoom`.
- Produces: `farsight-seed` flags:
  - `-url` (default `http://127.0.0.1:8080`);
  - `-server` (required);
  - `-token-env` (the name of the env var holding the agent token; default `FARSIGHT_SEED_TOKEN`; the token is never taken as a flag, so it doesn't show in `ps`);
  - `-snapshot FILE`;
  - `-events FILE`;
  - `-shift` (the events' and snapshot's `at`/`savedAt`/`since` are shifted so the newest event is `now − 1 min`, keeping relative times realistic and within the 13-day ingest window);
  - `-fake-tiles DATADIR`: writes a complete fake tile set for the snapshot's `world.seed`/`world.genVersion` at `tiles.SetDir(filepath.Join(DATADIR,"tiles"), seed, gen)`. Every tile of z0..z5 is a 256×256 PNG with a flat colour picked by quadrant, plus the `complete` marker containing `strconv.Itoa(tiles.RenderVersion)`. Run it **before** posting the snapshot so `Ensure` sees a complete set.

- [ ] **Step 1 (done in Task 4): Go test first** (`cmd/farsight-seed/main_test.go`):
  - `writeFakeTiles(dir, seed, gen)` writes exactly 1365 PNGs, and `tiles.Complete(dir)` is true;
  - `shiftTimes` moves the newest event to `now−1m` and preserves the gaps;
  - `run()` against an `httptest` server receives a gzip snapshot and events with the bearer from the env.
- [ ] **Step 2 (done in Task 4): Fixtures.** The fixtures use invented names only: Astrid, Bjorn, Sigrun, Ulf, Hilda, and a base "Longhouse".
  - `snapshot.json`: an `extract.Snapshot` for serverId `demo`, world "Demo", seed 12345, genVersion 2, day 214. It includes:
    - bosses with 4 defeated, and modifiers `{combat:hard, deathpenalty:casual, resources:more}`;
    - exploredZones covering a ~2 km disc around the origin plus a strip east;
    - markers: 5 portals (2 pairs + 1 unpaired "copper"), 2 beds, 1 tombstone, 3 tames (1 named "Big Mama" Lox, 1 Wolf, 1 Hen), 2 signs;
    - 2 bases with builders;
    - locations: Eikthyr and Yagluth altars, Haldor, SunkenCrypt4, TrollCave02, all inside explored zones, plus one crypt outside them (the server must filter it out).
  - `events.json`: `server_boot` (version 0.221.4), `server_ready`, `join_code` 318742, joins for Astrid, Bjorn and Sigrun, a leave for Ulf (with a join 40 min earlier), 3 `world_saved` 20 min apart, `players_now` 3, and heartbeats every 30 s over the last 2 min.
- [ ] **Step 3: Playwright config and global setup.**
  - `global-setup.ts`:
    - builds `farsight` with `-tags webui` (after `npm run build`) into a temp dir;
    - writes a config with 2 servers:
      - `demo` (crossplay, address `play.example.net:2456`, discordHint "ask in #demo", maxPlayers 10);
      - `quiet` (not crossplay, no snapshot);
    - uses passphrases `demo-pass` and `quiet-pass` and agent tokens `demo-token` and `quiet-token`, each hashed by piping it to `farsight hash` (cost 12; about 250 ms each, which is fine). The tokens reach `farsight-seed` via `FARSIGHT_SEED_TOKEN`;
    - sets `cookieSecure:false` and `listen 127.0.0.1:<free port>`;
    - runs `farsight-seed -fake-tiles`, starts `farsight serve`, waits for `/healthz`, then seeds `demo` snapshot and events;
    - exports `BASE_URL`;
    - teardown kills the process and removes the temp dir.
  - `playwright.config.ts` defines two projects:
    - `desktop`: 1440×900;
    - `mobile`: 390×844, `isMobile`, `hasTouch`.
    - It uses `chromium-headless-shell`, `retries: 1`, and screenshots on failure.
- [ ] **Step 4: E2E specs.** Use role- and text-based locators; no sleeps (use `expect.poll` and auto-waiting). Stub `navigator.clipboard` with `page.addInitScript`.
  - `desktop.spec.ts`:
    1. A fresh visit shows the unlock dialog. The wrong passphrase shows "Wrong passphrase."; the right one unlocks, and the side panel shows "Demo", "Online", `3/10` and version `0.221.4`.
    2. The share link `/#s=quiet&k=quiet-pass` unlocks `quiet`; the URL becomes `/#s=quiet` with no `k=`; the switcher lists both servers; switching back to demo works.
    3. Tiles load: at least one `img.leaflet-tile` has `naturalWidth` 256, and each tile request's URL contains `/tiles/demo/12345-2-r`.
    4. The Online tab lists Astrid, Bjorn and Sigrun; "Recently online" lists Ulf; the activity list shows "Astrid joined", with only one autosave row.
    5. World tab: day 214, "4 / 8", the boss grid has 4 filled discs, "Next up: Yagluth" is shown with "Show altar", and the world rules show Combat Hard highlighted.
    6. Search for "copper": one result, "Portal · unpaired"; picking it opens a MarkerCard with the "Unpaired" badge. Search for "Big" returns the tame.
    7. Layers: turn Beds on and a bed pin appears; turn Portals off and the portal pins and lines disappear.
    8. The paired portal card's "Jump to partner" selects the other portal.
    9. The join dialog shows "318 742"; Copy toasts "Join code copied"; the address box shows `play.example.net:2456`; the Steam-only `quiet` server shows no join code box.
    10. The theme toggle flips `data-theme` and persists across a reload.
    11. The crypt outside explored zones never appears anywhere (search "Sunken" finds exactly the one inside).
  - `mobile.spec.ts`:
    1. After unlock, the top bar shows "Demo" and the peek sheet "Online now", "3 / 10".
    2. Dragging the sheet up shows the pulled list with "Recently online".
    3. The menu opens search; typing "copper" and picking the result shows the docked card with a close button.
    4. Join opens the full-height join sheet with the code.
    5. Tapping the server name opens the server sheet.
  - `states.spec.ts` (desktop project) uses `page.route('**/api/servers/demo', …)` to return a modified copy of the real card:
    - offline → banner "Server offline · last seen online …" and the empty state "Server is resting";
    - online with an empty `online` list → "The longhouse is quiet";
    - tiles rendering 389/1365 → "Charting the world for the first time" and "389 of 1,365 tiles";
    - `world.savedAt` 4 h old → the stale banner;
    - tiles refused → "Can’t draw this world’s map yet".
- [ ] **Step 5: Run.** Run `make e2e`, i.e. `cd web && LD_LIBRARY_PATH="$(../hack/playwright-libs.sh --print)" npx playwright test`. Expected: all projects pass. Also run `go test ./... -count=1` and `cd web && npm run check && npm test`.
- [ ] **Step 6: Amend the spec** (`docs(spec): …`), updating these sections:
  - **Web UI:**
    - SvelteKit static build embedded behind the `webui` build tag, `make web farsight-ui`;
    - self-hosted fonts and no third-party requests;
    - the custom greedy clusterer (26 px) instead of marker clustering;
    - the unlock dialog;
    - the MVP adaptations for the design gaps, as a link to `design/DESIGN-NOTES.md` and a pointer to this plan's rulings table.
  - **Testing:** the Web bullet now names Vitest unit tests plus Playwright desktop and mobile projects against a seeded `farsight` (`cmd/farsight-seed`, fake tiles), and `hack/playwright-libs.sh`.
- [ ] **Step 7: Commit** `test(web): Playwright e2e for desktop and mobile`, then the spec commit.

---

## After this plan

Plan 5 (deployment) then:
- builds images with `npm ci && npm run build` and `go build -tags webui` (GOAMD64=v1, running `TestTerrainDigest` in the build);
- runs the Playwright suite in CI with Playwright's own `install --with-deps`;
- adds the cloudcluster manifests.

It also carries the Plan 4a follow-ups:
- a timeout on the bcrypt semaphore wait;
- tile-set GC by referenced keys;
- a per-server seed cache;
- session pruning;
- the ops note on passphrase rotation;
- keeping `/ingest/` off the public ingress.
