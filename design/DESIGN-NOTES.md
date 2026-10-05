# Farsight World Atlas: design digest

What this is: an implementation-grade digest of `design/World Atlas.dc.html` (the design canvas), `design/MarkerCard.dc.html` and the Organic design system (`design/_ds/organic-…/styles.css`, `readme.md`). It is checked against the MVP spec (`docs/superpowers/specs/2026-09-29-farsight-atlas-mvp-design.md`, "Web UI" and "API contract") and against the backend (`internal/extract`, `internal/server/api.go`, `internal/tiles/tiles.go`, `internal/logwatch`).

Conventions used here:
- `path:line` refers to `World Atlas.dc.html` unless it names another file.
- **[MVP]** means build it. **[OUT]** means a later sub-project (section 05 timeline, section 08 profiles, time and weather), or data the API does not provide. **[GAP]** means the design shows something the API cannot supply, and the fallback is given. **[AMBIG]** means the design is ambiguous and a decision is proposed.
- The design is a static canvas. Most numbers (4/10, 1.0.16, 318 742, "12 min ago") are hard-coded sample strings, not computed. The digest says so wherever it matters.

---

## 0. Canvas overview and file mechanics

- The canvas root (`:27`) is `padding:72px; display:flex; flex-direction:column; gap:88px; width:max-content`. Its intro block (`:29-32`) has an `h1` with `{{ appName }}` (default "Farsight") and this intro copy: "Farsight is a private companion for your Valheim servers: a world atlas plus who’s online. Each server has its own map, timeline and join details. The map is a snapshot rebuilt from each autosave; the online list is live. Positions of players are never shown."
- Section order in the file: **01 Desktop main view** (`:34-457`), **02 Mobile** (`:459-597`), **03 Marker details & world progression** (`:599-634`), **08 Profiles, world rules, time & weather** (`:636-745`), **07 Multiple servers** (`:747-773`), **06 How to join** (`:775-835`), **05 Activity timeline** (`:837-908`), **04 States** (`:910-958`). No other sections exist. **There is no unlock/passphrase screen in the design** ([GAP], see §3.21).
- Props (`data-props`, `:962`): `appName` (text, default "Farsight"), `theme` (`'dark' | 'light'`, default `dark`), `clustering` (boolean, default true), `serverName` (default "Example Vikings"), `timeOfDay` (morning/day/evening/night, default night) [OUT], `fogOfWar` (boolean, default true), `biomePatterns` (boolean, default true; a procedural-map-only detail, ignore it).
- Every screen frame sets `data-theme="{{ theme }}"`. The theme tokens (§2) exist only under `[data-theme]`.
- The map is a procedurally generated 1024×1024 canvas image (`genWorld`, `:1053-1108`), drawn as one `<img>` with a CSS `transform: translate(ox,oy) scale(z)` and `border-radius:50%`. Markers are absolutely positioned divs projected by `proj()` (`:1250-1279`). The real app replaces this with Leaflet `CRS.Simple` plus server tiles (spec).
- Icons are inline Lucide SVG paths in the `IC` table (`:966-997`), turned into `data:image/svg+xml` URLs by `svgUrl(name, colour)` with `stroke-width="2.75" stroke-linecap="round" stroke-linejoin="round"` and a baked-in stroke colour (`:998`). See §4.3 for the Lucide names.

---

## 1. Sections and screens

### 1.1 Section 01: Desktop main view [MVP]

The section header (`:35`) reads "01 · Desktop main view", with the subline "Interactive: drag to pan, scroll to zoom, click markers and clusters, search “swamp” or “Frøya”, toggle layers."

The frame (`:37`) is `position:relative; width:1440px; height:900px; overflow:hidden; border-radius:28px; background:var(--color-bg); color:var(--color-text); font-family:var(--font-body); box-shadow:0 30px 80px rgba(0,0,0,.28)`. In the app this is the full viewport, without the radius or the outer shadow.

**Z-order, bottom to top, in DOM order since no element sets an explicit z-index except the server dropdown `z-index:5` and the sticky timeline headers `z-index:1`:**

1. **Map layer** (`:38-51`): `position:absolute; inset:0; overflow:hidden; cursor:grab|grabbing; touch-action:none; user-select:none`. Inside it, in order:
   1. The world image (`:40`): 1024×1024, `border-radius:50%`, `box-shadow:0 0 0 5px color-mix(in srgb,var(--color-text) 12%,transparent), 0 30px 80px rgba(0,0,0,.35)`, `filter:{{ mapFilter }}`, `transition:filter .4s`, `pointer-events:none`. The fog is baked into this image (§5.5).
   2. Ward circles (`:42`) [OUT: the backend emits no wards].
   3. Portal connection lines (`:43`).
   4. Pins: marker discs and cluster bubbles (`:44-50`), each with an optional base label under it.
2. **"Charting…" placeholder** (`:53-55`), shown while `!ready`: `position:absolute; left:50%; top:50%; transform:translate(-30%,-50%); padding:18px 24px; border-radius:28px; background:var(--glass); border:1px solid var(--color-divider); font-size:14px`, copy "Charting Example from the world save…" (hard-codes "Example"; use the server name). The `-30%` x-offset nudges it right of centre to clear the side panel.
3. **Side panel variants.** Exactly one of these renders:
   - Timeline aside (`:57-115`) [OUT, section 05]: `left:16px; top:16px; bottom:16px; width:520px; border-radius:30px; background:var(--color-surface)`.
   - Profile aside (`:116-185`) [OUT, section 08]: same box, `width:344px`, surface background.
   - **Normal side panel** (`:186-291`) [MVP]: `left:16px; top:16px; bottom:16px; width:344px; display:flex; flex-direction:column; border-radius:30px; background:var(--glass); backdrop-filter:blur(14px); border:1px solid var(--color-divider); box-shadow:var(--shadow-lg); overflow:hidden`. Contents in §3.3–§3.10.
   - **Collapsed panel pill** (`:292-299`) [MVP], when `!panelOpen`: see §3.4.
4. **"Map updated" pill** (`:301-308`) at `top:16px; left:{chrome.pillL}px`, where `pillL` = 376 with the panel open (16 + 344 + 16), 190 with it closed, and 552 with the timeline open. See §3.11.
5. **Time and weather pill** (`:309-336`) at `top:106px; left:{pillL}` [OUT, section 08].
6. **Top-right cluster** (`:338-386`): `position:absolute; top:16px; right:16px; display:flex; gap:10px; align-items:flex-start`. It holds the search box (340 px wide, §3.12), then the Layers button and its dropdown (§3.13).
7. **Marker popover** (`:388-392`): `position:absolute; left:{pop.l}px; top:{pop.t}px; width:312px`, containing `MarkerCard` with close and jump handlers (§3.16 and §5.9).
8. **Scale and cursor readout** (`:394-399`): `left:{chrome.readL}px; bottom:16px`, where `readL` = 376 with the panel open, 16 closed, 552 with the timeline open. See §3.14.
9. **Zoom controls** (`:401-407`): `right:16px; bottom:16px`. See §3.15.
10. **Join dialog** (`:409-452`), a modal over everything (§3.19).
11. **Toast** (`:453-455`): `left:50%; bottom:84px; transform:translateX(-50%)` (§3.20).

The **visible map centre** for programmatic moves is x=900 (panel open) or x=720 (closed), and y=470 (`centerOn`, `:1156`; zoom buttons, `:1480`). That is the centre of the area right of the panel. In Leaflet, pass `paddingTopLeft: [376, 0]` or offset the centre by (376/2) px when the panel is open.

### 1.2 Section 02: Mobile [MVP]

The header (`:460`) reads "02 · Mobile", with the subline "Map fills the screen. Players live in a bottom sheet; search and layers open from the top-bar menu but land at thumb height."

Frames are laid out as a row with `gap:40px`. Each phone is `position:relative; width:390px; height:844px; border-radius:48px; overflow:hidden; background:var(--color-bg); box-shadow:0 0 0 10px #0f0e0d, 0 30px 60px rgba(0,0,0,.3)` (the device chrome; drop it in the app). The map transform is `m1` (`:1433`): zoom 1.35, centred on the home base at screen (195, 400).

Four frames:

1. **"Default · sheet peeking"** (`:462-502`).
   - Map (with `filter:{{ nightFilter }}`, [OUT]; use none), then portal lines and pins, non-interactive in the design.
   - **Top bar** at `left:12px; right:12px; top:54px` (§3.17).
   - **Time pill** at `left:12px; top:122px`, height 44 [OUT, section 08].
   - **Zoom buttons** at `right:14px; bottom:208px`: a column with `gap:10px` of three 52×52 circles, `background:var(--glass); border:1px solid var(--color-divider); box-shadow:var(--shadow-md)`, with icons plus, minus and reset at 20 px.
   - **Peek sheet** (`:485-500`) at `left:0; right:0; bottom:0; padding:10px 16px 30px; border-radius:30px 30px 0 0; background:var(--glass); backdrop-filter:blur(14px); border-top:1px solid var(--color-divider); box-shadow:var(--shadow-lg); gap:14px`. It computes to about 161 px tall. See §3.18.
2. **"Sheet pulled up"** (`:504-537`). The map is dimmed with `filter:brightness(.7)`. The top bar has no shadow. The sheet sits at `top:176px` to the bottom, with `background:var(--color-surface)` (opaque, not glass) and `gap:6px`.
3. **"Menu · search & layers"** (`:539-572`). The map is dimmed with `brightness(.55)`. The top-bar menu button becomes a close button (`background:var(--cold)`, x icon in `--color-bg` colour). A bottom sheet with auto height holds the search field, hint tags, a Layers grid, and a "Show portal connections" row (§3.18).
4. **"Marker tapped"** (`:574-595`). The map is at zoom 2.2, centred on the unpaired "copper" portal at screen (195, 300), which is shown selected. `MarkerCard` sits at `left:12px; right:12px; bottom:28px` (366 px wide). **No close button is passed** (`onClose` absent), so the card has none. [AMBIG] Proposal: add the close button, and also close on a map tap.

### 1.3 Section 03: Marker details and world progression [MVP]

The header (`:600`) reads "03 · Marker details & world progression", with the subline "Popovers on desktop, floating cards on mobile. Everything is “as of the last save”."

The frame (`:601`) is `width:1440px; padding:36px; border-radius:32px; display:flex; flex-wrap:wrap; gap:28px; background:var(--color-bg)`. It is a gallery of 312-px-wide `MarkerCard`s, each with a label (`font-size:12px; font-weight:700; letter-spacing:.08em; text-transform:uppercase; color:var(--muted)`). The eight gallery entries (`:1438`) are "Portal · paired", "Portal · unpaired", "Location · dungeon", "Tombstone", "Tamed creature", "Base", "Boss altar · defeated" and "Boss altar · not defeated". It ends with a **"World progression panel"** card (344 px wide; `padding:22px; border-radius:30px; background:var(--color-surface); border:1px solid var(--color-divider); box-shadow:var(--shadow-lg); gap:18px`) that reuses the World-tab blocks (§3.9), without the "Forsaken defeated" heading and without "Next up".

### 1.4 Section 04: States [MVP]

The header (`:911`) reads "04 · States", with the subline "Offline, nobody online, first-run map generation, and stale data."

The layout is `display:grid; grid-template-columns:repeat(2,720px); gap:28px`. Each state is a 720×460 frame (`border-radius:28px; overflow:hidden; box-shadow:0 20px 50px rgba(0,0,0,.25)`) with its map at zoom 0.42, centred at (480, 230), plus:

- **Mini panel** (`:926`): `left:12px; top:12px; bottom:12px; width:236px; padding:14px; border-radius:24px; background:var(--glass); border:1px solid var(--color-divider); box-shadow:var(--shadow-md); gap:12px`. This is a scaled-down stand-in for the side panel. In the app, the real side panel shows the same state.
- **Banner** (`:940-945`): `left:262px; right:12px; top:12px; padding:12px 16px; border-radius:22px; box-shadow:var(--shadow-md); gap:12px`.
- **Progress card** (`:946-953`): `left:330px; top:150px; width:320px; padding:20px 22px; border-radius:28px; background:var(--color-surface); border:1px solid var(--color-divider); box-shadow:var(--shadow-lg); gap:10px`.

In the app, place the banner at the top of the map area, right of the side panel (`left:376px; right:16px; top:16px`), probably below or replacing the map-updated pill. [AMBIG] The design never shows the banner together with the full desktop chrome. Proposal: the banner replaces the map-updated pill when present, and the progress card is centred in the map area. Full state details are in §3.22 and §6.

### 1.5 Section 05: Activity timeline [OUT]

`:837-908`. It has a desktop 520-px aside and a mobile full-height sheet, plus the "Where each entry comes from" note. It is not built in the MVP. In the MVP, the "Full timeline →" links in the side panel and the mobile sheet **must be omitted**.

### 1.6 Section 06: How to join [MVP]

The header (`:776`) reads "06 · How to join", with the subline "The join code is read from the server log’s “registered with join code” line, so it’s current after every restart. Desktop: “How to join” in the server card. The platform switch here is live."

- **Mobile join sheet** (`:778-823`): the map is dimmed with `brightness(.55)`. The sheet runs from `top:54px` to the bottom (`border-radius:30px 30px 0 0; background:var(--color-surface); box-shadow:var(--shadow-lg); overflow:hidden`), with a 44×5 grab handle (`margin:10px 0 4px`) and a scroll area `padding:6px 16px 30px; gap:14px`. The content matches the desktop dialog, with touch sizes: platform buttons `min-height:44px`, code at 44 px instead of 52, buttons `min-height:48px`, and a 48-px round close button (`background:color-mix(in srgb,var(--color-text) 8%,transparent)`) instead of `btn-icon`.
- **"Join code states"** column (`:824-833`), 380 px wide, holds the three `codeStates` cards (§3.19).
- The desktop version is the modal in §1.1, item 10.

### 1.7 Section 07: Multiple servers [MVP]

The header (`:748`) reads "07 · Multiple servers", with the subline "Each server keeps its own map, timeline, progression and join code. Desktop: the “servers” pill in the server card. Mobile: tap the server name in the top bar."

- **Mobile server switcher** (`:750-771`): the map is dimmed with `brightness(.55)`. The top bar gets `border:1px solid var(--cold)`, a chevron-up after the name, the subline "Choose a server", and no menu button. The bottom sheet (`padding:10px 16px 30px; gap:8px; background:var(--color-surface)`) holds the handle, the title "Servers" (heading, 22 px, `padding:0 4px 4px`), the server rows (§3.5), and the footer "Archived worlds stay browsable as read-only maps." [OUT: archives are not in the MVP; drop the footer].
- The desktop switcher is the dropdown in the server card (§3.5).

### 1.8 Section 08: Profiles, world rules, time and weather [OUT, except the world rules card]

`:636-745`. It contains the mobile player profile, the time and weather dropdown, and the **world rules card**. The world rules card is reused in the World tab and the join dialog, so **it is in the MVP** (spec 06: "world rules card"). Also out: the night map tint (`nightFilter`), the time pill and the "Profile →" links.

---

## 2. Theme

### 2.1 Base: Organic tokens (`styles.css :root`)

- Colours: `--color-bg #f5ead8`, `--color-surface #ebddc5`, `--color-text #201e1d`, `--color-accent #c67139`, `--color-accent-2 #7a8a5e`, `--color-divider color-mix(in srgb,#201e1d 16%,transparent)`.
- Ramps 100–900 for `neutral`, `accent` and `accent-2` (values in `styles.css:12-41`).
- Fonts: `--font-heading "Caprasimo"` (weight 400), `--font-body "Figtree"` (400/600/700), both from Google Fonts via `@import`.
- Spacing: `--space-1…8` = 4.4, 8.8, 13.2, 17.6, 26.4, 35.2 px.
- Radii: `--radius-sm 8px`, `--radius-md 16px`, `--radius-lg 28px`.
- Shadows: `--shadow-sm 0 1px 2px color-mix(#2e2b25 14%)`, `--shadow-md 0 3px 10px color-mix(#2e2b25 16%)`, `--shadow-lg 0 12px 32px color-mix(#2e2b25 22%)`.
- Body: `font-size:15px; line-height:1.55`. Headings use Caprasimo with `line-height:1.12; letter-spacing:-.015em`.
- Component classes used by the design: `.btn`, `.btn-primary`, `.btn-secondary`, `.btn-ghost`, `.btn-icon` (36×36), `.btn-block` (`width:100%; margin-top:var(--space-2)`), `.input`, `.tag`, `.tag-neutral`, `.tag-accent-2`. Note that `.btn` renders in **Caprasimo** (heading font) at 14 px. The design overrides this to `font-family:var(--font-body)` on some ghost buttons ("Profile →", "Reset filters"), but **not** on "Full timeline →", "How to join", "Copy code", "Show altar" or "Jump to partner →", which therefore render in Caprasimo. `.btn`, `.tag` and `.input` get `border-radius:999px`. `.input` gets `padding-inline:14px`.
- Focus: `:focus-visible { outline:2px solid var(--color-accent); outline-offset:2px }`. The README requires Lucide icons at stroke-width 2.75, `:hover` tints, and pressed states from the accent ramp.

### 2.2 Canvas-level style block (`:14-25`)

```css
body{margin:0;background:var(--color-neutral-200)}          /* canvas only */
a{color:var(--color-accent-700)}a:hover{color:var(--color-accent-800)}
[data-theme="dark"]{
  --color-bg:#161513;--color-surface:#22201c;--color-text:#efe6d6;--color-accent:#d67f48;
  --color-divider:color-mix(in srgb,#efe6d6 13%,transparent);
  --color-neutral-100:#2e2b25;--color-neutral-200:#474238;--color-neutral-300:#645c50;--color-neutral-400:#82796a;--color-neutral-500:#a19786;--color-neutral-600:#c0b6a5;--color-neutral-700:#dcd3c4;--color-neutral-800:#eee7db;--color-neutral-900:#f9f4ed;
  --color-accent-100:#402310;--color-accent-200:#643312;--color-accent-300:#8c491a;--color-accent-400:#b2622d;--color-accent-500:#d67f48;--color-accent-600:#f6a06b;--color-accent-700:#ffc6a5;--color-accent-800:#ffe1d0;--color-accent-900:#fff2eb;
  --color-accent-2-100:#272e1b;--color-accent-2-200:#3d472b;--color-accent-2-300:#56633f;--color-accent-2-400:#728157;--color-accent-2-500:#8fa073;--color-accent-2-600:#aebf92;--color-accent-2-700:#ccdbb2;--color-accent-2-800:#e1eecc;--color-accent-2-900:#f0fae1;
  --shadow-sm:0 1px 2px rgba(0,0,0,.4);--shadow-md:0 4px 14px rgba(0,0,0,.45);--shadow-lg:0 14px 40px rgba(0,0,0,.55);
  --cold:#7fb2d6;--cold-ink:#b5d4ea;--glass:rgba(30,28,25,.9);--muted:color-mix(in srgb,#efe6d6 64%,transparent)}
[data-theme="light"]{--cold:#2f6c94;--cold-ink:#1d4a69;--glass:rgba(245,234,216,.92);--muted:color-mix(in srgb,#201e1d 64%,transparent)}
[data-theme] .btn-secondary{color:var(--color-text)}
```

Notes:
- Dark **inverts the ramps**: step 100 is dark and step 900 is light. That is why the same `var(--color-accent-700)` reads as dark-orange text on light and pale-peach on dark. `--color-accent-2` itself is **not** overridden in dark (it stays #7a8a5e).
- The app-specific tokens `--cold`, `--cold-ink`, `--glass` and `--muted` exist **only** under `[data-theme]`, so the app root must always carry `data-theme`. MarkerCard falls back to `var(--cold-ink, #1d4a69)` and `var(--muted, rgba(32,30,29,.62))`.
- `--cold` is the "data from the save / map" colour: the map pill progress bar, the layer toggle track, the selection ring, search hover and the mobile top-bar dot. `--cold-ink` is used for link-like text ("Profile →", "Map →", the "Or join by address" kicker, the MarkerCard kicker, cold badges).

### 2.3 Theme-derived JS colours (baked into icon data URIs)

From `renderVals` (`:1315-1317`, `:1383`). In Svelte, express these as CSS variables with a `color` prop on Lucide icons:

| JS name | dark | light | Equivalent token (both themes) |
|---|---|---|---|
| `T` (icon ink) | `#efe6d6` | `#201e1d` | `var(--color-text)` |
| `BG` (icon on accent/cold) | `#161513` | `#f5ead8` | `var(--color-bg)` |
| `coldC` | `#7fb2d6` | `#2f6c94` | `var(--cold)` |
| `EM`/`EM0` (ember icon) | `#f6a06b` | `#b2622d` | `var(--color-accent-600)` |
| `SG0`/moonSage | `#ccdbb2` | `#56633f` | `var(--color-accent-2-700)` |
| stale banner icon | `#ffe1d0` | `#643312` | `var(--color-accent-800)` |

Theme-independent constants (`:965`): `INK #26231f`, `CREAM #f5ead8`, `COLD #3d7eab`, `EMBER #c67139`, `SAGE #7a8a5e`. Marker discs deliberately use these fixed colours in both themes.

### 2.4 Default and switching

- Default is `dark` (the `theme` prop default). The effective theme is `state.themeOverride || props.theme || 'dark'` (`:1314`).
- The toggle button in the side-panel header (`toggleTheme`, `:1471`) flips `themeOverride`. Its icon is **sun** when dark and **moon** when light (`ic.theme`), with `aria-label="Toggle theme"`.
- The design has no persistence and no `prefers-color-scheme`. Proposal for the MVP: default to dark, persist the override in `localStorage` (wrapped in try/catch), and put `data-theme` on `<html>`. On mobile the design shows **no theme toggle** at all. [AMBIG] Proposal: add it to the mobile menu sheet.

---

## 3. Components

Recurring atoms:
- **Disc**: `border-radius:50%; display:grid; place-items:center`.
- **Soft fill**: `background:color-mix(in srgb,var(--color-text) 6%,transparent)` (also 7%, 8% or 9% in places, noted per item).
- **Row hover**: `style-hover="background:color-mix(in srgb,var(--color-text) 6%,transparent)"`.
- **Section label**: `font-size:11px; color:var(--muted); text-transform:uppercase; letter-spacing:.08em`.

### 3.1 App mark (brand disc)

- Desktop panel: a 38×38 disc with `background:var(--color-accent); color:var(--color-bg)`, heading font 20 px, text `{{ appInitial }}` (first letter of `appName`, uppercased: "F").
- Collapsed pill: 32 px disc, 17 px letter. Mobile top bar: 40 px disc, 20 px letter.

### 3.2 Side panel header (`:188-193`)

`display:flex; align-items:center; gap:12px; padding:18px 16px 14px 18px`, containing:
- the app mark;
- a title block (`flex:1; min-width:0`): `{{ appName }}` (heading, 20 px, `line-height:1.1`), then the subline "Valheim server atlas" (12 px, muted);
- `button.btn.btn-secondary.btn-icon` with `aria-label="Toggle theme"` and a 17 px sun or moon icon;
- `button.btn.btn-secondary.btn-icon` with `aria-label="Collapse panel"` and a 17 px chevron-left icon.

### 3.3 Server card (`:194-224`)

Container: `margin:0 14px; padding:16px; border-radius:24px; background:color-mix(in srgb,var(--color-text) 6%,transparent); display:flex; flex-direction:column; gap:12px`.

1. **Status row** (`:195-199`), `gap:8px`:
   - a dot: `width:10px; height:10px; border-radius:50%; background:var(--color-accent); box-shadow:0 0 0 4px color-mix(in srgb,var(--color-accent) 25%,transparent)`;
   - the label "Online" (13 px, bold, `color:var(--color-accent-700)`);
   - "Live status" on the right (`margin-left:auto`, 12 px, muted).

   The design shows only the online state here. Proposed status mapping, from the 04 States palette and the join-code states:

   | `Card.status` | dot | label | label colour |
   |---|---|---|---|
   | `online` | `--color-accent` with ring | "Online" | `--color-accent-700` |
   | `starting` | `--cold` | "Starting" [AMBIG, not in design] | muted |
   | `restarting` | `--cold` | "Restarting" (join-code state kicker) | muted |
   | `offline` | `--color-neutral-500`, no ring | "Offline" | `--muted` |
   | `unknown` | `--color-neutral-500` | "Unknown" [AMBIG] | `--muted` |
2. **Server switcher button**: see §3.5.
3. **Stats grid** (`:218-222`): `grid-template-columns:repeat(3,minmax(0,1fr)); gap:8px`, three cells, each a label (11 px, muted, uppercase, `.08em`) over a value:
   - "Players": `4` in 18 px bold tabular, then `<span muted, weight 400>/10</span>`. Source: `Card.players` / `Card.maxPlayers`.
   - "Version": `1.0.16` (15 px bold, `padding-top:3px`). Source: `Card.version`; show "—" if absent.
   - "Uptime": `3d 6h` (15 px bold, `padding-top:3px`). Source: now − `Card.upSince`; "—" if absent or offline.
4. `button.btn.btn-primary.btn-block` "How to join" (`white-space:nowrap`) opens the join dialog.

### 3.4 Collapsed panel pill (`:292-299`)

`button` at `left:16px; top:16px; height:48px; display:flex; align-items:center; gap:10px; padding:0 16px 0 8px; border-radius:999px; border:1px solid var(--color-divider); background:var(--glass); box-shadow:var(--shadow-md)`, containing:
- the 32 px app mark;
- a 9 px dot (`--color-accent`);
- "4 online" (14 px, bold). Pattern: `{players} online`;
- a 16 px chevron-right.

Clicking it re-opens the panel.

### 3.5 Server switcher

**Trigger** (`:201-204`): a transparent full-width button with `aria-haspopup="listbox"`, `gap:8px`, containing:
- the server name (heading, 22 px, `line-height:1.1`, ellipsis);
- a pill: `height:28px; padding:0 8px 0 11px; border-radius:999px; background:color-mix(in srgb,var(--color-text) 9%,transparent); gap:4px; font-size:12px; font-weight:700`, text "`{serverCount} servers`" followed by a 14 px chevron-down.

**Dropdown** (`:205-216`), `role="listbox"`: `position:absolute; z-index:5; top:38px; left:-10px; right:-10px; padding:8px; border-radius:22px; background:var(--color-surface); border:1px solid var(--color-divider); box-shadow:var(--shadow-lg); gap:2px`.
- **Row**: a button with `gap:10px; padding:9px 10px; border-radius:16px`. Background is `color-mix(in srgb,var(--cold) 14%,transparent)` for the current server and transparent otherwise, with a 7% text hover. It contains:
  - a 9 px dot: `--color-accent` if the server is live, `--color-neutral-500` if archived or offline;
  - the name (14 px, bold) over a sub-line (12 px, muted);
  - the count on the right (12 px, bold, muted, tabular).
- **Footer**: "Admins add servers in Settings. Each gets its own map, timeline and join code." (`padding:8px 10px 4px; font-size:12px; border-top:1px solid var(--color-divider); margin-top:4px`). [GAP] The MVP has no Settings; servers come from config and are listed only once unlocked. Proposed copy: "Only servers you’ve unlocked are listed."

**Mobile variant** (07, `:761-767`): rows with `min-height:68px; padding:0 14px; border-radius:22px; gap:12px`. The current row has `background:color-mix(--cold 14%)` and `border:1.5px solid var(--cold)`; others are transparent with a divider border. Dot 11 px, name 16 px bold, sub 13 px muted, count 14 px bold tabular.

Sample rows (`:1353`): `['Example Vikings','Day 214 · 4 of 8 bosses','var(--color-accent)','4/10']`, `['Ashen Crew','Day 61 · 1 of 8 bosses',…,'1/10']`, `['Example (2024)','Archived world · read-only map','var(--color-neutral-500)','—']`. Picking a non-current server in the prototype shows the toast "Prototype: only Example Vikings has data".

[GAP] `ServerSummary` is `{id,name,status,players,maxPlayers}`, with no day or bosses. Fallback: the sub-line shows the status word ("Online", "Offline", "Restarting"…) for non-current servers. The current server can use its full Card: "Day {world.day} · {defeated} of {bosses.length} bosses". Count is `"{players}/{maxPlayers}"`, or "—" when offline. [AMBIG] Should the "N servers" pill show with one server? Proposal: show it as "1 server" (singular); it still explains that more can exist.

### 3.6 Panel tabs (`:225-228`)

Container: `display:flex; gap:6px; margin:14px 14px 4px; padding:4px; border-radius:999px; background:color-mix(in srgb,var(--color-text) 6%,transparent)`.

Two `flex:1` buttons: `border:0; border-radius:999px; padding:8px 0; font-size:14px; font-weight:700`. The active one has `background:var(--color-surface); color:var(--color-text)`; the inactive one is transparent with `color:var(--muted)`. Labels: "Online · 4" (`Online · {players}`) and "World". The default tab is `players`. The scroll body below is `flex:1; overflow:auto; padding:10px 14px 18px`.

### 3.7 Online and recent players list (Players tab, `:230-247`)

Wrapper: `flex-direction:column; gap:2px`.
- **Intro** (12 px, muted, `padding:4px 6px 8px`): "Live from the server. Player positions aren’t tracked."
- **Online row**: `display:flex; align-items:center; gap:12px; padding:8px; border-radius:18px; cursor:pointer`, with a hover of text 6%. It contains:
  - Avatar: 40 px disc, `background:var(--color-accent-200); color:var(--color-accent-800)`, heading 18 px, the name's initial. On top of it, an online dot `position:absolute; right:-1px; bottom:-1px; 13×13; background:var(--color-accent); border:2.5px solid var(--color-surface)`.
  - Name (15 px, bold) over "`online {session}`" (13 px, muted, tabular). Samples: `1h 12m`, `38m`, `5m`, `2h 40m`.
  - "Profile →" `btn btn-ghost` (body font 12 px bold, `--cold-ink`) **[OUT]**. Drop it; rows are not clickable in the MVP.
- **"Recently online"** label (12 px, muted, `padding:12px 6px 4px`).
- **Recent row**: same layout, but the avatar uses `background:var(--color-neutral-200); color:var(--color-neutral-800)` with no dot, and the sub-line is "last seen 24 min ago" (`last seen {relative} ago`). "Profile →" is [OUT].

API mapping:
- `online[]` gives name, `since`, and `session` = now − since. The API sorts it `since asc`, so the longest session is first. The design sample order (Johnny 1h12m, Alina 38m, Halvor 5m, Frøya 2h40m) is unsorted, so follow the API.
- `recent[]` gives name and `until`. [AMBIG] It can contain several sessions per player, and players who are online now. Proposal: dedupe by name keeping the newest `until`, exclude names currently online, and cap at 5.
- Empty online list: see "Nobody online" (§3.22).

### 3.8 Recent activity list (`:248-255`)

- **Header row**: `justify-content:space-between; padding:18px 6px 6px`, with "Recent activity" (heading, 17 px) and "Full timeline →" (`btn btn-ghost`, 13 px, bold, `--cold-ink`). The link is **[OUT]**.
- **Row**: `display:flex; align-items:flex-start; gap:10px; padding:7px 8px; border-radius:14px`, hover text 5%, `cursor:pointer` only when it has a map target. It contains a 28 px icon disc (`background:{a.bg}`, 14 px icon), the text (14 px, `line-height:1.35`, `padding-top:4px`, `flex:1`), and the time (12 px, muted, nowrap, `padding-top:5px`).

Sample (`:1384-1390`):

| icon (colour) | disc bg | text | time |
|---|---|---|---|
| log-in (EM) | `--color-accent-100` | Halvor joined | 5 min |
| skull (EM) | accent-100 | New tombstone near the Swamp (Halvor) | save 14:20 (clickable, centres on it) [OUT: save-derived] |
| log-out (T) | `color-mix(text 8%)` | Bjørn left | 24 min |
| log-in (EM) | accent-100 | Alina joined | 38 min |
| log-in (EM) | accent-100 | Johnny joined | 1 h 12 m |
| flame (SG) | `--color-accent-2-100` | Bonemass defeated | day 198 [OUT: save-derived] |

The spec says this list shows **only log events** in the MVP. Mapping from `Card.activity[].type` (the backend emits `server_starting`, `server_boot`, `server_ready`, `server_stopped`, `join_code`, `player_join`, `player_leave`, `world_saved`; `heartbeat` and `players_now` are excluded by the API):

| type | icon | tone (disc bg / icon colour) | copy (design source or proposal) |
|---|---|---|---|
| `player_join` | log-in | ember: accent-100 / accent-600 | "{name} joined" (design) |
| `player_leave` | log-out | neutral: text 8% / text | "{name} left" (design). The timeline form is "{name} left after 3h 05m", using `seconds` |
| `server_ready` | power | neutral | "Server restarted · version {version}" (timeline copy `:1027`). Proposal: "Server is up · version {version}" |
| `server_stopped` | power | neutral | proposal: "Server stopped" |
| `server_starting` / `server_boot` | power | neutral | proposal: "Server starting" |
| `world_saved` | map [AMBIG; the timeline uses the `server` type → power] | neutral | "Autosave finished · map updated" (timeline `:1012`) |
| `join_code` | power [AMBIG] | neutral | proposal: "New join code {code}" (format as `318 742`) |

Mobile shows only the first 3 items (`activityShort`). [AMBIG] `world_saved` can dominate the list (every ~20 min). Proposal: collapse consecutive saves, or show only the latest save.

### 3.9 World progression (World tab, `:258-288`, and the 03 panel)

Wrapper: `flex-direction:column; gap:18px; padding:8px 6px`.

1. **Two stat tiles** (`grid 1fr 1fr; gap:10px`), each `padding:14px 16px; border-radius:22px; background:text 6%`:
   - label "In-game day" (12 px, muted) over `214` (heading, 34 px, `line-height:1.1`). Source: `Card.world.day`.
   - "Bosses" over `4<span style="font-size:20px;color:var(--muted)"> / 8</span>`. Source: count of `world.bosses[].defeated` / `bosses.length`.
2. **"Forsaken defeated"** (heading, 17 px, `margin-bottom:10px`; only in the World tab), then a boss grid: `grid-template-columns:repeat(8,minmax(0,1fr)); gap:4px`. Each cell is a column with `gap:6px`, centred:
   - a 38×38 disc (heading font, 14 px, `border:2px`, `box-sizing:border-box`) with the roman numeral I–VIII. Defeated: `background:var(--color-accent); color:var(--color-bg); border:2px solid var(--color-accent)`. Not defeated: transparent, `color:var(--muted); border:2px dashed color-mix(in srgb,var(--color-text) 35%,transparent)`;
   - the name (10.5 px, `line-height:1.2`), `--color-text` if defeated and muted otherwise. Short names: `{'The Elder':'Elder','The Queen':'Queen','Kall Fimbulbringer':'Kall'}`.
   - The design's boss list is `['Eikthyr','The Elder','Bonemass','Moder','Yagluth','The Queen','Fader','Kall Fimbulbringer']`. **Valheim has only seven bosses; there is no 8th.** `defeated_writhan` (`tables.go`) is not a boss key — it's the progress key the game sets when the Writhan, an ordinary Swamp creature, is killed, like `killedtroll`. Use the API's seven names. Short-name map: The Elder → Elder, The Queen → Queen, anything else as is.
3. **"Next up" box** (`:275-278`): `display:flex; align-items:center; gap:10px; padding:12px 14px; border-radius:20px; border:1px dashed var(--color-divider)`. It holds the text "Next up: <b>Yagluth</b>, whose altar is in the Plains." (13 px, `line-height:1.4`) and `btn btn-secondary` "Show altar" (13 px), which centres the map on the altar at design zoom 3 and selects it (`:1474`).
   - The next boss is the first undefeated one in order. [GAP] Biome is not in the API. Use a static map: Eikthyr → Meadows, The Elder → Black Forest, Bonemass → Swamp, Moder → Mountains, Yagluth → Plains, The Queen → Mistlands, Fader → Ashlands, Writhan → Deep North.
   - Show "Show altar" only if a `boss_altar` location with that label is in the snapshot. Locations are filtered to explored zones. [AMBIG] Several altar locations can exist (for example, multiple GDKing or Dragonqueen altars). Proposal: pick the one nearest the world centre, or the first.
   - When all bosses are defeated, hide the box. [AMBIG, not designed]
4. **World rules card**: §3.10.
5. **Explored bar** (`:286`), `gap:6px`: a row "Explored by the crew" + `<b tabular>{exploredPct}%</b>` (13 px, `space-between`), then a track (`height:6px; border-radius:999px; background:text 12%`) with a fill of `width:{pct}%; background:var(--color-accent-2-500)`. Source: `Card.world.exploredPct` (1 decimal; the design rounds to an integer).
6. **Footnote** (12 px, muted, `line-height:1.45`): "Bosses come from the world’s progress keys. Exploration comes from the zones the server has generated."

With no `Card.world` (no snapshot yet), hide the World tab contents and show a proposed empty state: "No world save read yet." [AMBIG]

### 3.10 World rules card (`:280-286`, `:443-449`, `:624-630`, `:733-740`)

`flex-direction:column; gap:8px`:
- **Header** (`align-items:baseline; justify-content:space-between`): "World rules" (heading, 16 px) and `{{ worldPreset }}` (12 px, muted); sample "Custom · based on Normal".
- **Grid** `repeat(2,minmax(0,1fr)); gap:6px`. Each tile is `padding:8px 12px; border-radius:14px; background:text 6%`, with the key (11.5 px, muted) over the value (13.5 px, bold, `color:{fg}`). `fg` = `var(--color-accent-700)` if changed from Normal, else `var(--color-text)`.
- **Footnote** (11.5 px, muted): "Orange = changed from Normal. Read from the world’s modifier keys."
- In section 08 it stands alone in a `padding:18px; border-radius:26px; surface; border; shadow-lg` card, with the caption "Shown in the World tab and in How to join, so new players know what they’re signing up for."

Sample (`:1416`), as `[key, value, changed]`: Combat / Hard / 1 · Death penalty / Casual · keep gear / 1 · Resources / 1.5× / 1 · Raids / Less often / 1 · Portals / Ores & metals allowed / 1 · Map / Enabled / 0 · Passive enemies / Off / 0 · No build cost / Off / 0.

API mapping: `Card.world.modifiers` is a `{name: value}` map parsed from the world's `preset combat_hard:deathpenalty_casual:…` starting key (`extract.worldInfo`). `Card.world.flags` holds other starting keys, such as `nomap`, `passivemobs` and `nobuildcost`. Proposed display table (unverified against real saves [AMBIG]):

| Tile | Source | Normal value | Value labels |
|---|---|---|---|
| Combat | `modifiers.combat` | Normal | veryeasy → "Very easy", easy → "Easy", hard → "Hard", veryhard → "Very hard" |
| Death penalty | `modifiers.deathpenalty` | Normal | casual → "Casual · keep gear", veryeasy/easy/hard, hardcore → "Hardcore" |
| Resources | `modifiers.resources` | 1× | muchless → 0.5×, less → 0.75×, more → 1.5×, muchmore → 2×, most → 3× |
| Raids | `modifiers.raids` | Normal | none → "None", muchless/less → "Less often", more/muchmore → "More often" |
| Portals | `modifiers.portals` | Normal | casual → "Ores & metals allowed", hard → "No boss portals", veryhard → "No portals" |
| Map | flag `nomap` | Enabled | "Disabled" when present |
| Passive enemies | flag `passivemobs` | Off | "On" |
| No build cost | flag `nobuildcost` | Off | "On" |

`worldPreset` = "Normal" when no modifier and no flag differs from Normal; otherwise "Custom · based on Normal". [AMBIG] The design never shows another preset name.

### 3.11 "Map updated" pill (`:301-308`)

Container: `position:absolute; top:16px; left:{pillL}px; display:flex; flex-direction:column; gap:8px; padding:10px 16px 12px; border-radius:24px; background:var(--glass); border:1px solid var(--color-divider); box-shadow:var(--shadow-md); min-width:360px`.
- **Row 1** (`gap:10px; font-size:14px`): a 17 px map icon in `--cold`, then `<b>Map updated 12 min ago</b><span style="color:var(--muted)"> · next save in ~8 min</span>` (nowrap).
- **Row 2**, the save-cycle bar: `height:5px; border-radius:999px; background:text 12%`, with a fill of `width:60%; background:var(--cold)`. That is 12/(12+8), so the fill = elapsed / saveInterval, clamped to 100%.
- **Row 3** (11.5 px, muted): "Snapshot from the 14:32 autosave · markers don’t move between saves". 14:32 is the local HH:MM of `world.savedAt`.

Formulas:
- `N` = floor((now − `world.savedAt`)/60 s).
- `M` = ceil((`saveIntervalSec` − elapsed)/60).
- When `saveIntervalSec` is absent (fewer than 3 saves), drop " · next save in ~M min" and the bar. When `M ≤ 0` but not yet stale, proposal: " · next save any moment" [AMBIG].
- The pill re-renders every 30–60 s.
- Mobile short form: "Map 12 min ago · next ~8 min" (§3.17).

### 3.12 Search box and results (`:339-359`)

- **Wrapper**: `position:relative; width:340px`.
- **Icon**: a search icon at `position:absolute; left:16px; top:14px; 18×18; opacity:.7; pointer-events:none`.
- **Input** (`.input`): `height:46px; padding-left:44px; background:var(--glass); box-shadow:var(--shadow-md); font-size:14px`, placeholder **"Portals, signs, tames, builders…"**. Focus opens the results and closes the layers menu; blur closes the results after 120 ms.
- **Panel** (when focused): `position:absolute; top:54px; left:0; right:0; padding:10px; border-radius:24px; background:var(--color-surface); border:1px solid var(--color-divider); box-shadow:var(--shadow-lg); gap:2px; max-height:420px; overflow:auto`.
  - **Empty query**: the label "Try" (12 px, muted, `padding:6px 8px 4px`), then hint chips (`button.tag.tag-neutral`, 13 px, `padding:5px 12px`, `border:0`) that set the query on `mousedown`. Sample hints: `swamp`, `copper`, `Frøya`, `Big Mama`, `trolls`. [GAP] These are sample-specific. Proposal: derive up to 5 from real data (the first portal tag, a named tame, a base builder, a boss name), or show no hints.
  - **Result row**: `gap:12px; padding:8px; border-radius:16px`, hover `color-mix(in srgb,var(--cold) 16%,transparent)`, picked on `mousedown`. It holds a 32 px disc (`background:{pin bg}; border:2px solid #f5ead8`) with the 16 px marker icon, then the title (14 px, bold) over the sub-line (12 px, muted).
  - **No match**: "Nothing matches “{q}” in the last save." (14 px, muted, `padding:10px 8px`).
- Behaviour: §5.2.

### 3.13 Layers button and panel (`:360-385`)

- **Button**: `height:46px; gap:8px; padding:0 18px; border-radius:999px; border:1px solid {divider, or --cold when open}; background:var(--glass); box-shadow:var(--shadow-md); font-size:14px; font-weight:700`, containing an 18 px layers icon, "Layers", and a count badge (`min-width:22px; height:22px; padding:0 7px; border-radius:999px; background:var(--cold); color:var(--color-bg); font-size:12px`). The count is the number of enabled layers among the 10 (portal links excluded); the default is 7.
- **Panel**: `position:absolute; top:54px; right:0; width:318px; padding:12px; border-radius:26px; background:var(--color-surface); border:1px solid var(--color-divider); box-shadow:var(--shadow-lg); gap:2px`.
  - **Title**: "Map layers" (heading, 17 px, `padding:4px 10px 8px`).
  - **Layer row** (a button): `gap:12px; padding:7px 10px; border-radius:16px`, hover text 6%. It contains:
    - a 28 px disc with `background:#26231f; opacity:{1 on / .35 off}`, holding a 14 px icon in cream;
    - the label (14 px, `line-height:1.2`) with an optional sub-line (block, 11.5 px, colour below);
    - the count (12 px, muted, tabular);
    - a switch: track `38×22; border-radius:999px; background:{on: var(--cold) / off: color-mix(text 22%)}`, and a knob `16×16` cream (`#f5ead8`), `top:3px; left:{19 on / 3 off}px; box-shadow:0 1px 2px rgba(0,0,0,.3)`.
  - **Portals sub-row** "Show connections" (`role="checkbox"`, `aria-checked`): `margin:-2px 0 4px 40px; padding:6px 10px; border-radius:14px; font-size:13px; gap:10px`. The checkbox is `18×18; border-radius:6px; border:2px solid {on: --cold / off: color-mix(text 40%)}; background:{on: --cold / off: transparent}`, holding a 12 px check icon in the `--color-bg` colour. The count reads "`{pairs} pairs`". When Portals is off, the row gets `opacity:.4; cursor:not-allowed` and does nothing.
  - **Legend**: `grid repeat(3,minmax(0,1fr)); gap:6px 8px; padding:12px 10px 4px; margin-top:6px; border-top:1px solid var(--color-divider)`. Each entry is a 14 px swatch (`border:1px solid rgba(0,0,0,.2)`) plus the name (12 px). The entries are the 9 biomes plus "Unexplored" `#cfbe9c`. **Use the tile renderer's palette, not the design's** (`internal/tiles/tiles.go:72-82`): Meadows `#A3B25C`, Black Forest `#3E5834`, Swamp `#786246`, Mountains `#E2E6EC`, Plains `#D6BE6E`, Mistlands `#6E6478`, Ashlands `#963C28`, Deep North `#C8D7E6`, Ocean `#2E5478`. The design's sample RGBs (`:964`) are Ocean (40,70,98), Meadows (140,170,92), Black Forest (46,82,66), Swamp (104,88,64), Mountains (230,228,222), Plains (228,196,108), Mistlands (124,106,150), Ashlands (166,62,44) and Deep North (190,216,234).
- **Layer table** (`LAYERS`, `:1002`; defaults `:1112`):

| key | label (desktop) | short (mobile) | icon | default | sub-line (design) |
|---|---|---|---|---|---|
| biomes | Biomes & terrain | Terrain | mountain | on | — (count shows 9) |
| structures | Structures & bases | Bases | home | on | — |
| portals | Portals | Portals | portal | on | "1 unpaired" (`--color-accent-700`) |
| beds | Beds | Beds | bed | **off** | — |
| tombstones | Tombstones | Tombstones | skull | on | "1 new since 14:20" (accent-700) [GAP: needs a diff; drop] |
| tames | Tamed creatures | Tames | paw | on | — |
| vehicles | Ships & carts | Ships & carts | ship | on | [OUT: not emitted] |
| wards | Wards | Wards | shield | **off** | [OUT: not emitted] |
| signs | Signs | Signs | sign | **off** | — |
| locations | Dungeons & locations | Locations | arch | on | "Only where someone has been" (muted) |
| portalLinks | (sub-row) Show connections | Show portal connections | check | on | "{n} pairs" |

- For the MVP, drop `vehicles` and `wards`, keeping 8 rows. The portals sub-line becomes "`{n} unpaired`" (portals without `pair`), hidden when n = 0. [AMBIG] Should zero-count layers be hidden? Proposal: show them with count 0.
- Turning "Biomes & terrain" **off** does not hide the map. It greys it out: `mapFilter` = `grayscale(1) contrast(.85) brightness(1.08)` (`:1447`).

### 3.14 Scale and cursor readout (`:394-399`)

Pill: `left:{readL}px; bottom:16px; gap:14px; padding:9px 16px; border-radius:999px; background:var(--glass); border:1px solid var(--color-divider); box-shadow:var(--shadow-md); font-size:13px; tabular-nums`. It contains:
- the scale bar: a `height:6px; width:{scale.w}px; border:2px solid var(--color-text); border-top:0` bracket over its label (11 px, muted);
- a 1 px divider;
- the coordinates (`<b>`, `min-width:170px`), default "X — · Z —";
- the biome (muted, `min-width:90px`), default "Hover the map". Other values: "World edge" (outside the disc), "Unexplored" (fog on and zone unexplored), or the biome name.

[GAP] The client has no biome data. The spec's API serves tiles only. Proposal: show "Unexplored" or nothing in that slot, or drop the biome column. Coordinates come from the Leaflet mouse position (§7.3). Hide the readout on touch devices (the design's mobile has none).

Scale algorithm: §5.7.

### 3.15 Zoom controls (`:401-407`)

A column pill at `right:16px; bottom:16px` (`border-radius:999px; background:var(--glass); border; shadow-md; overflow:hidden`). Three `btn btn-icon` buttons, each `46×46; border-radius:0`, with the 2nd and 3rd getting `border-top:1px solid var(--color-divider)` and a hover of text 8%:
- "Zoom in" (plus, 18 px);
- "Zoom out" (minus);
- "Reset view" (rotate-ccw).

Behaviour: §5.6.

### 3.16 MarkerCard (`MarkerCard.dc.html`)

Props: `m` (card model), `onClose?`, `onJump?(partnerId)`. Preview size 340×420; used at 312 px (desktop popover and gallery) or 366 px (mobile).

- **Root**: `width:100%; display:flex; flex-direction:column; gap:14px; padding:18px 18px 16px; border-radius:28px; background:var(--color-surface); color:var(--color-text); border:1px solid var(--color-divider); box-shadow:var(--shadow-lg); font-family:var(--font-body)`.
- **Header** (`align-items:center; gap:12px`):
  - Disc: `44×44; flex:none; border-radius:50%; background:{m.discBg}; border:2px solid #f5ead8; box-shadow:0 2px 6px rgba(0,0,0,.25)`, holding a 21×21 icon.
  - Text block (`flex:1; min-width:0`):
    - Kicker: `font-size:11px; letter-spacing:.1em; text-transform:uppercase; font-weight:700; color:var(--cold-ink, #1d4a69)`.
    - Title: `font-family:var(--font-heading); font-size:21px; line-height:1.15; overflow-wrap:anywhere`.
  - Close (only if `onClose`): `button.btn.btn-secondary.btn-icon`, `aria-label="Close"`, `34×34; align-self:flex-start`, 16 px x icon.
- **Badges** (if any): `display:flex; gap:6px; flex-wrap:wrap`. Each is a `span.tag` with `background:{b.bg}; color:{b.fg}; font-weight:700; font-size:12px`. Tones (`TONE`, `:1003`):
  - `cold`: bg `color-mix(in srgb, var(--cold) 22%, transparent)`, fg `var(--cold-ink)`
  - `ember`: `var(--color-accent-100)` / `var(--color-accent-800)`
  - `sage`: `var(--color-accent-2-100)` / `var(--color-accent-2-800)`
  - `neutral`: `var(--color-neutral-100)` / `var(--color-neutral-800)`
- **Facts** (if any): `display:grid; grid-template-columns:auto minmax(0,1fr); column-gap:16px; row-gap:6px; font-size:14px`. The key is `color:var(--muted, rgba(32,30,29,.62))`; the value is `font-weight:600; tabular-nums; overflow-wrap:anywhere`.
- **Items** (chest contents only) [OUT]: `padding:4px 14px; border-radius:18px; background:text 6%`, with rows `name … ×count`.
- **Note** (if any): a `<p>` with `margin:0; font-size:13.5px; line-height:1.5; text-wrap:pretty; opacity:.88`.
- **Jump** (if `partnerId`): `button.btn.btn-primary` "Jump to partner →" (`white-space:nowrap`) in a wrapper with `padding-top:2px`.
- **Where line**, always shown: `font-size:12px; color:var(--muted); tabular-nums`, format `X {x} · Z {z} · {biome}` (§6.4).

Per-kind content: §4.2.

### 3.17 Mobile top bar (`:474-478`)

`position:absolute; left:12px; right:12px; top:54px; display:flex; align-items:center; gap:8px; padding:6px 6px 6px 8px; border-radius:999px; background:var(--glass); border:1px solid var(--color-divider); box-shadow:var(--shadow-md)`. `top:54px` clears the iPhone status bar; in the app use `calc(env(safe-area-inset-top) + 8px)`.
- The 40 px app mark.
- A title block (`flex:1; min-width:0`) with two lines:
  - `serverName` (heading, 17 px, `line-height:1.1`, nowrap) plus a 14 px chevron-down (`gap:4px`). Tapping it opens the server switcher sheet;
  - a sub-line (12 px, muted, `gap:6px`): a 7 px `--cold` dot, then "Map 12 min ago · next ~8 min". Other frames omit the dot.
- **Menu button**: 48×48 disc, `background:color-mix(in srgb,var(--color-text) 8%,transparent)`, 20 px menu icon. When the menu is open: `background:var(--cold)` with the x icon in the `--color-bg` colour.
- **Switcher-open state**: border `1px solid var(--cold)`, chevron-up, the sub-line "Choose a server", and no menu button.

### 3.18 Mobile bottom sheet and menu

**Grab handle**: `width:44px; height:5px; border-radius:999px; background:color-mix(in srgb,var(--color-text) 28%,transparent); align-self:center`.

**Peek** (`:485-500`), which is glass with a blur:
1. The handle.
2. A header row (`gap:10px`):
   - "Online now" (heading, 21 px);
   - a count pill: `height:28px; padding:0 11px; border-radius:999px; background:var(--color-accent); color:var(--color-bg); font-weight:700; font-size:14px`, text "4 / 10";
   - a Join pill at `margin-left:auto`: `height:44px; padding:0 16px; border-radius:999px; background:var(--color-accent); color:var(--color-bg); font-weight:700; font-size:14px`, text "Join". It opens the join sheet.
3. A horizontal chip scroller (`gap:8px; overflow-x:auto; scrollbar-width:none; margin-right:-16px; padding-right:16px`). Each chip is `padding:6px 12px 6px 6px; border-radius:999px; background:text 7%; gap:8px`, with a 32 px avatar (accent-200/800, heading 15 px), then `<b 14px>name</b>` over the session (11.5 px, muted). The session reads e.g. "1h 12m", with no "online" prefix.

**Pulled up** (`:513-535`), surface background at `top:176px`, `gap:6px`:
1. The handle (`margin-bottom:8px`).
2. The header "Online now" (heading 22 px) with the "4 / 10" pill (no Join button here), `padding:0 4px`.
3. A sub-line (12.5 px, muted, `padding:0 4px 6px`): "`{serverName} · live · positions aren’t tracked`".
4. Player rows: `min-height:60px; gap:12px; padding:0 4px`, with a 44 px avatar (heading 19 px) and online dot, the name (16 px bold) over "online {session}" (13.5 px, muted), and a right-hand "`{n} bases`" / "`1 base`" (13 px bold `--cold-ink`, `padding:12px 4px`). [GAP] Base count per online player needs name matching (§7.2); proposal: omit when 0 or unmatched.
5. "Recent activity" (heading 18 px) with "Full timeline →" [OUT], then 3 activity rows (`min-height:48px; gap:12px`; 32 px disc, 15 px icon; text 14.5 px; time 12 px muted).

[AMBIG] The recently-online list does not appear on mobile. Proposal: add it under the online list, as on desktop.

**Menu sheet** (`:548-570`): `padding:18px 16px 30px; gap:12px; surface`, no handle. It contains:
- A search field: an icon at `left:18px; top:16px`, 20 px, `opacity:.7`, and a `.input` with `height:52px; padding-left:48px; font-size:16px`. 16 px avoids iOS zoom.
- Hint tags (`.tag.tag-neutral`, 14 px, `padding:8px 14px`, `gap:8px`, wrapping).
- "Layers" (heading, 18 px, `padding-top:6px`).
- A 2-column grid (`gap:8px`) of layer tiles. Each tile is `min-height:52px; padding:0 12px 0 8px; border-radius:18px; gap:10px`, with `background:{on: color-mix(--cold 16%) / off: transparent}; border:1.5px solid {on: --cold / off: divider}`. It holds a 32 px `#26231f` disc (`opacity` 1 or .35) with a 15 px cream icon, and the short label (13 px, 600). There are no counts on mobile.
- A "Show portal connections" row: `min-height:48px; padding:0 12px; border-radius:18px; border:1.5px solid var(--color-divider); gap:12px`, with a 22 px checkbox (`border-radius:7px`, 14 px check), the label (14 px, 600) and "{n} pairs" (12 px, muted).

[AMBIG] Mobile search results are not drawn. Proposal: reuse the desktop result rows inside the menu sheet, replacing hints and layers while a query is typed. Picking a result closes the menu and selects the marker.

**Sheet snap points**: the design has **no sheet logic**; it has static frames only. The implied snaps are:
1. **peek**: auto height, about 161 px. The zoom buttons sit at `bottom:208px` to clear it.
2. **half/pulled**: `top:176px`.
3. **full**: `top:54px`, used for the join sheet (and the out-of-scope profile and timeline).

Proposal: drag between peek and pulled; join and server sheets open at full or auto height. Dimming behind raised sheets: `brightness(.7)` for pulled, `brightness(.55)` for menu, join and server sheets.

### 3.19 How to join (desktop dialog `:409-452`, mobile sheet `:778-823`)

- **Backdrop**: `position:absolute; inset:0; background:rgba(10,9,8,.55); display:grid; place-items:center`. A click closes it.
- **Dialog** (`role="dialog"`, `aria-label="How to join"`): `width:540px; max-height:840px; overflow:auto; display:flex; flex-direction:column; gap:16px; padding:24px; border-radius:32px; background:var(--color-surface); border:1px solid var(--color-divider); box-shadow:var(--shadow-lg)`.

1. **Header** (`align-items:flex-start; gap:12px`):
   - "`Join {serverName}`" (heading, 26 px, `line-height:1.1`);
   - a status line (13 px, muted, `margin-top:4px`, `gap:8px`): an 8 px `--color-accent` dot, then "Online · 4/10 · crossplay on". Pattern: `{Status} · {players}/{max} · crossplay {on|off}`;
   - a close `btn btn-secondary btn-icon` with a 16 px x.
2. **Platform segmented control**: `gap:4px; padding:4px; border-radius:999px; background:text 7%`. Buttons are `flex:1; border:0; border-radius:999px; min-height:34px; padding:0 6px; font-size:13px; font-weight:700`. Active: `background:var(--color-surface); color:var(--color-text)`; inactive: transparent, muted. Labels: "PC (Steam)", "Xbox", "PS5", "Switch 2". Default `pc`.
3. **Join code box**: `gap:10px; padding:18px; border-radius:24px; background:color-mix(in srgb,var(--color-accent) 12%,transparent); border:1.5px solid color-mix(in srgb,var(--color-accent) 40%,transparent)`, containing:
   - kicker row: "Join code" (11 px, `.1em`, uppercase, 700, `--color-accent-700`), and at `margin-left:auto` an 8 px accent dot (`box-shadow:0 0 0 3px color-mix(accent 25%)`) with "Live from server" (12 px, muted);
   - code row (`gap:12px; flex-wrap:wrap`): "318 742" (heading, **52 px**, `line-height:1; letter-spacing:.06em; tabular-nums`), and `btn btn-primary` "Copy code" (`margin-left:auto; min-height:40px`) with a 16 px copy icon in the `--color-bg` colour;
   - note (12.5 px, muted, `line-height:1.45`): "Issued at today’s 06:00 restart. A new code is issued every time the server restarts, and this page updates by itself." The pattern is "Issued at {today’s|<date>} {HH:MM} restart.", using `Card.joinCodeAt`.
4. **Address box** (PC tab only): `gap:12px; padding:14px 16px; border-radius:22px; background:text 6%`, containing:
   - kicker "Or join by address (PC)" (11 px, `.1em`, uppercase, 700, `--cold-ink`);
   - the address "play.example.net:2456" (17 px bold tabular, `overflow-wrap:anywhere`) from `Card.address`;
   - "PC only. Stays the same across restarts." (12 px, muted);
   - `btn btn-secondary` "Copy" (`min-height:40px`) with a 15 px copy icon.

   Hide the box when `address` is absent.
5. **Steps**, a numbered list (`gap:10px`). Each row has `gap:12px`: a 26 px number disc (`background:var(--color-accent-2-200); color:var(--color-accent-2-800)`, 700, 13 px) and the text (14 px, `line-height:1.45`, `padding-top:3px`).
   - PC: "Start Game, pick your character, then Start." / "Open the Join Game tab and choose Join IP." / "Paste the join code or the address below, then Connect." / "Enter the server password."
   - Xbox, PS5 and Switch 2 (identical): "Start Game and pick your character." / "Open Join Game and add a server by join code." / "Type the 6-digit code." / "Enter the server password."
6. **Info box**: `gap:6px; padding:12px 14px; border-radius:18px; border:1px dashed var(--color-divider); font-size:13px; line-height:1.45`, with these lines:
   - "<b>Password:</b> ask in the Discord <b>#example</b> channel. It’s never shown here." The bold part comes from `Card.discordHint`. [AMBIG] `discordHint` is free text; proposal: "**Password:** {discordHint}. It’s never shown here.", or, when absent, "**Password:** ask the server admin. It’s never shown here."
   - "<b>Version:</b> your game must be on <b>1.0.16</b>, the same as the server." (`Card.version`; hide the line if absent).
   - Consoles only (muted): "Crossplay must be allowed in your console’s privacy settings."
7. **World rules** card (§3.10).

**Join code states** (`codeStates`, `:1348-1352`). Card: `gap:8px; padding:16px 18px; border-radius:24px; background:surface; border:1.5px solid {border}`. It holds a kicker row (kicker 11 px muted uppercase; status with an 8 px dot, right-aligned), the code (heading 34 px, `letter-spacing:.05em`) and a note (12.5 px, muted).

| state | kicker | status | dot | border | code (colour) | note |
|---|---|---|---|---|---|---|
| live | Live | From server log | accent | `color-mix(accent 45%)` | `318 742` (text) | Read from “registered with join code 318742” at 06:00. Shown with a Copy button. |
| restarting | Restarting | Waiting for new code | `--cold` | `--cold` | `— — —` (muted) | The old code is hidden as soon as the log shows a shutdown, so nobody copies a dead one. A new code usually appears within a minute. |
| offline | Offline | Last seen 03:12 | `--color-neutral-500` | divider | `No code` (muted) | There’s no active session to join. The address stays listed for PC players and will work once the server is back. |

These are reference variants. In the real dialog the join-code box takes the matching state:
- `status=online` with `joinCode` → live;
- `status ∈ {starting, restarting}` → restarting;
- `offline` or `unknown` → offline, with "Last seen {lastHeartbeat HH:MM}".

[GAP] **`crossplay=false`** (Steam-only server) is not designed. It has no join code. Proposal: hide the join-code box and the platform switch, and show the address box as the primary (with the kicker "Join by address"), plus the PC steps. [AMBIG] Also undesigned: `online` without a `joinCode` yet. Use the restarting look with "Waiting for code".

**Copy**: copies the raw code "318742" (no space), then shows the toast "Join code copied" with the sub-line "318 742". Address copy shows the toast "Address copied" with the address.

### 3.20 Toast (`:453-455`)

`position:absolute; left:50%; bottom:84px; transform:translateX(-50%); flex-direction:column; align-items:center; gap:2px; padding:12px 20px; border-radius:22px; background:var(--color-text); color:var(--color-bg); box-shadow:var(--shadow-lg); font-size:14px`. It shows `<b>{title}</b>` over `<span 12px opacity .75 tabular>{sub}</span>`. It auto-hides after **2600 ms** (`:1158`). The default title is "Link copied".

### 3.21 Unlock and passphrase screen [GAP, not in design]

The spec requires `POST /api/unlock {server, passphrase}`, share links with `#s=…&k=…`, and a 404 for locked servers. Nothing is designed for this. Proposal, built from existing atoms:
- An Organic `.dialog` (`width:min(440px,100%)`, `radius-lg*1.15`, surface, shadow-lg) centred over a blurred, dimmed empty map background (`--color-bg`).
- Contents: the app mark and appName; the title "Unlock a server" (`.dialog-title`); fields `.field` "Server" (`.input`, the server id) and "Passphrase" (`.input type=password`); `btn btn-primary` "Unlock".
- Errors: 401 → "Wrong passphrase."; 429 → "Too many attempts. Try again in a few minutes."
- The same dialog is reachable from the server switcher as "Add a server…".
- When no server is unlocked, it is the whole screen.

### 3.22 State banners, empty panel and charting progress (04 States, `:1440-1445`)

Mini-panel pieces:
- **Status row**: a 10 px dot, the status (13 px, bold, coloured), and the count on the right (12 px, muted).
- **Server name**: heading 19 px.
- **Player rows**: a 30 px avatar (heading 14 px), the name (13.5 px bold), the session (12 px muted).
- **Empty state** (`flex:1; justify-content:center; align-items:flex-start; gap:10px; padding:0 4px`): a 56 px disc (`background:var(--color-accent-2-100)`) with a 24 px moon icon in accent-2-700, the title (heading 20 px, `line-height:1.15`), and the body (13 px, muted, `line-height:1.45`).

| State | Map treatment | Pin opacity | Status dot / text / colour / count | Panel content | Banner |
|---|---|---|---|---|---|
| **Server offline** | `filter:grayscale(.55) brightness(.8)` | .75 | `--color-neutral-500` / "Offline" / muted / "—/10" | empty: **"Server is resting"**, "Nobody can join until it’s back. The map shows the final save before shutdown (03:10)." | bg `var(--color-text)`, fg `var(--color-bg)`, power icon in the BG colour. Title: **"Server offline · last seen online today 03:12 (11 h ago)"**. Body: "The map is still here to browse. We’ll switch back to live as soon as it answers." |
| **Nobody online** | none | 1 | accent / "Online" / accent-700 / "0/10" | empty: **"The longhouse is quiet"**, "Nobody’s online right now. Johnny was the last to leave, 2 h ago. The fire’s still warm." | none |
| **Map generating · first run** | the low-res 40 px image, `blur(3px) saturate(.8)`, `image-rendering:pixelated` | hidden | accent / "Online" / "4/10" | players list | Progress card (below) |
| **Stale map data** | `filter:sepia(.35) brightness(.9)` | .85 | accent / "Online" / "4/10" | players list | bg `var(--color-accent-100)`, fg `var(--color-accent-800)`, triangle-alert icon in accent-800. Title: **"Map last updated 3 h 41 min ago · saves usually every ~20 min"**. Body: "Autosave may be failing, or the world file isn’t being read. Markers may be out of date; the online list is still live." |

**Progress card** ("Charting"):
- Title: "Charting the world for the first time" (heading 21 px, `line-height:1.15`).
- Bar: `height:8px`, track text 12%, fill `width:38%; background:var(--cold)`.
- Row (13 px, tabular, `space-between`): `<b>38% · 389 of 1,024 tiles</b>` and "~2 min left" (muted).
- Body (12.5 px, muted): "Tiles sharpen as they finish. Markers appear once the first pass is done. The online list already works."

API and behaviour notes:
- Offline = `Card.status === 'offline'`. "last seen online today 03:12 (11 h ago)" uses `Card.lastHeartbeat`. The "(03:10)" in the empty body is `world.savedAt` HH:MM.
- Nobody online = `status online` with `online.length === 0`. "Johnny was the last to leave, 2 h ago." uses `recent[0]` (`until desc`). If `recent` is empty, drop that sentence. Also drop "The fire’s still warm." when the last leave is more than about 6 h ago. [AMBIG; proposal]
- Stale = `now − world.savedAt > 2 × saveIntervalSec` (spec). "~20 min" = `saveIntervalSec/60`, rounded. When `saveIntervalSec` is absent, stale cannot be computed. Proposal: use a 2 h threshold and the title "Map last updated {dur} ago" without the "saves usually" clause. [AMBIG]
- Charting = `Card.tiles.state ∈ {queued, rendering}`. `%` = `round(done/total*100)`. "389 of 1,024 tiles" uses en-US grouping. `total` is 1365 (all zoom levels 0–5, `tiles.go:51`), **not** 1,024; the design's 1,024 is a single-zoom number.
  - [GAP] "~2 min left": the API has no ETA. Compute it client-side from the `done` rate between polls, or omit it until two samples exist.
  - [GAP] The low-res preview: tiles are only served once the whole set is complete (the `/tiles/…` key only exists for a complete set), so there is no partial tile to show. Proposal: show the plain `--color-bg` background, or a generic disc placeholder, under the card.
  - [AMBIG] "Markers appear once the first pass is done": the snapshot is independent of tiles. Proposal: follow the design and hide markers until the tiles are complete.
- `tiles.state === 'refused'` (unknown genVersion) and `'none'` are not designed. Proposal: a banner in the stale style, titled "Can’t draw this world’s map yet", with the body "This world was made by a newer game version than Farsight knows. Markers and the online list still work." [AMBIG]
- No snapshot at all (`GET …/snapshot` 404, no `Card.world`): proposal: reuse the desktop "Charting…" pill copy as "Waiting for the first world save…". [AMBIG]

---

## 4. Markers

### 4.1 Disc styles (`pinStyle`, `:1241-1249`; `proj`, `:1260-1272`)

All pins are `border-radius:50%; border:2px solid {border}; box-sizing:border-box; display:grid; place-items:center; cursor:pointer`, centred on the point (left = x − s/2). The icon size is `round(s × .52)`. The default shadow is `0 2px 6px rgba(0,0,0,.35)`.

| design type | size s | disc bg | border | icon colour | icon px | extra |
|---|---|---|---|---|---|---|
| base | 36 | CREAM `#f5ead8` | INK `#26231f` | INK | 19 | label under the disc when design zoom ≥ 0.7 |
| portal (paired) | 28 | COLD `#3d7eab` | CREAM | CREAM | 15 | |
| portal (unpaired) | 28 | COLD | CREAM | CREAM | 15 | ring shadow `0 0 0 3px #c67139, 0 0 0 7px rgba(198,113,57,.35)` |
| tomb | 28 | EMBER `#c67139` | CREAM | CREAM | 15 | |
| altar | 30 | SAGE `#7a8a5e` if defeated, else INK | CREAM | CREAM | 16 | |
| everything else (bed, tame, sign, trader, crypt, cave, burial, ward, chest, ship, cart) | 26 | INK `#26231f` | CREAM | CREAM | 14 | |

- **Selected pin**: shadow `0 0 0 4px #7fb2d6, 0 0 0 8px rgba(127,178,214,.35), 0 4px 12px rgba(0,0,0,.4)`. The colour is fixed at the dark `--cold`, not the token. The selected pin is never clustered.
- **Base label**: `position:absolute; top:100%; left:50%; transform:translateX(-50%); margin-top:4px; white-space:nowrap; font-family:var(--font-heading); font-size:13px; color:#201e1d; background:rgba(245,234,216,.9); padding:1px 9px; border-radius:999px; pointer-events:none`. The text is the base title.
- **Tooltip** (`title` attribute): "`{Kicker} · {title}`", for example "Portal · swamp1".

### 4.2 Kinds: kicker, icon, facts, badges, note

`card()` (`:1281-1310`). For every card: kicker = `KICK[type]`, title = `m.title`, icon = `TICON[type]` in the pin's icon colour, `discBg` = the pin bg, and where = `X {fmtN(wx)} · Z {fmtN(wz)} · {biome}`.

| design type | Kicker | Lucide icon | Badges | Facts (key → value) | Note | MVP source |
|---|---|---|---|---|---|---|
| portal (paired) | Portal | *custom* "portal" (two concentric circles; §4.3) | "Paired" (cold) | Tag → `“{tag}”`; Partner → `{partner biome} · {km} km {dir8}` (computed; the far end of a hub pair reads "Skovheim portal hall · {km} km {dir}") | hub: "One of 14 portals in Skovheim’s portal hall."; far end: "Leads back to the portal hall at Skovheim." (sample-specific) | `markers[kind=portal]`: `label` = tag, `pair` = partner id |
| portal (unpaired) | Portal | portal | "Unpaired" (ember) | Tag → `“copper”` | "No other portal carries the tag “copper”, so this one leads nowhere. Build a partner with the same tag, or retag it." | `pair` absent |
| base | Base | home (house) | — | Builder → name; Pieces → `2,184`; Also built by → "Johnny, Alina, Halvor" (or "—"); Wards → "1 active" | "Builder = whoever placed the most pieces here." | `bases[]` |
| tomb | Tombstone | skull | "New since last check" (ember, if fresh) | Player; Appeared → "after save at 14:20"; Items inside → 31 | "Saves only show it wasn’t there at 14:00 and was at 14:20. The exact time of death isn’t recorded." | `markers[kind=tombstone]`, `owner` |
| tame | Tamed creature | paw-print | "Tamed" (sage) | Name; Species; Level → `★★  (2-star)` or "No stars"; Near → base name | — | `markers[kind=tame]`: `label` = TamedName, `species` = prefab |
| altar | Boss altar | flame | "Defeated" (sage) or "Not yet defeated" (neutral) | Forsaken → boss; Biome → biome | defeated: "The world remembers this victory. Its trophy hangs at Skovheim."; not: "Still waiting for a brave crew." | `locations[kind=boss_altar]`, `label` = boss name |
| sign | Sign | signpost | — | Placed by → name | "Sign text as of the last save." Title = the raw text (no quotes; the pin tooltip uses `“text”`) | `markers[kind=sign]`, `label` = text |
| bed | Bed | bed | — | Owner | "Spawn point for {owner}." | `markers[kind=bed]`, `owner` |
| trader | Trader | coins | — | Sells → "Gear, upgrades, pocket expansions" (Haldor) / "Clothing & cosmetics" (Hildir) / "Ingredients & trinkets" (Bog Witch) | "Trader camp. Shown because someone has been here." | `locations[kind=trader]`, `label` |
| crypt | Sunken crypt | *custom* "arch" | — | — | "Shown because someone has explored this area. Locations in unexplored areas stay hidden." | `locations[kind=dungeon]` |
| cave | Frost cave | mountain | — | — | same default note | `dungeon` |
| burial | Burial chamber | arch | — | — | same default note | `dungeon` |
| ward [OUT] | Ward | shield | — | Owner; At; Radius → "32 m" | — | not emitted |
| chest [OUT] | Chest | box | — | Container; In; items list | "Contents as of the 14:32 save." | not emitted |
| ship / cart [OUT] | Ship / Cart | ship / *custom* cart | — | Near | "Position as of the last save; it may have sailed since." | not emitted |

MVP adaptations ([GAP] items):
- **Where line**: the biome is not available. Use `X {fmtN(x)} · Z {fmtN(z)}`. The backend coordinates are `x` (east) and `z` (north); `y` is height, not shown.
- **Portal partner**: `{km} km {dir8}`, dropping the biome. For the note, use "Leads to the “{tag}” portal {km} km {dir}." Drop the hub note; it is sample-specific.
  - **Unpaired semantics differ**: the backend pairs only when *exactly two* portals share a tag (`pairPortals`). Three or more portals with the same tag are all "unpaired" even though others carry the tag. Proposal copy:
    - 1 portal with the tag: the design note.
    - 3 or more: badge "Unpaired" plus the note "{n} portals share the tag “{tag}”, so the game can’t pair them."
    - Empty tag: title "Untagged portal", note "This portal has no tag, so it leads nowhere."
- **Base**:
  - Title = `name`. Default `"{top builder}'s base"`, or "Base" when the builder is unknown.
  - Builder = `builders[0].name || "Unknown builder"`.
  - Pieces = `pieces.toLocaleString('en-US')`.
  - Also built by = the other builders' names, joined with ", ", with unknown ones shown as "Unknown builder" and deduped, or "—".
  - Drop Wards.
  - Keep the note.
  - Pin position = `(x, z)`. `radius` is available but not drawn in the design. [AMBIG] Optionally draw a faint circle.
- **Tombstone**: facts: Player → `owner` ("Unknown" if empty). Drop the badge, Appeared, Items inside and the timing note (all need save diffing). Proposal note: "Where {owner} died. It stays until they recover their items." [AMBIG] Title = owner name, as in the design (`title: 'Halvor'`).
- **Tame**: title = `label` (TamedName), or the species if unnamed (tamed-but-unnamed animals are included, e.g. `Hen`). Facts: Name (only if named), Species (`species` is the prefab name, e.g. "Wolf", "Lox", "Boar", "Hen", "Asksvin"; show as is), Near → the nearest base name within some radius, computed client-side (proposal: ≤ 300 m, else omit). Drop Level. Keep the "Tamed" badge.
- **Altar**: `defeated` is joined from `Card.world.bosses` by `name === label`; the names match between `bossAltars` and `bossKeys`. Facts: Forsaken → label; drop Biome, or use the static boss → biome map from §3.9. Defeated note: "The world remembers this victory." (drop the Skovheim sentence).
- **Sign**: drop "Placed by". An empty sign text shows the title "Blank sign".
- **Bed**: an empty `owner` shows "Unknown". The note becomes "Spawn point for someone." [AMBIG]
- **Dungeon**: kicker and title from `label` (e.g. "Sunken crypt", "Burial chambers", "Troll cave", "Frost cave", "Bear cave", "Howling cavern", "Smouldering tomb", "Sealed tower", "Fuling village"). Icon by `type` (§8). The default note, as is.

### 4.3 Lucide icon inventory

Every icon in `IC` (`:966-997`), identified by its path data. Use `lucide-svelte` with `stroke-width={2.75}`; the colour comes from CSS `currentColor`.

| IC key | Lucide name | Used for |
|---|---|---|
| portal | **none (custom)**: `circle r9` + `circle r4`. Closest Lucide: `circle-dot` (r10 + r1) or `target`. Proposal: keep a custom inline SVG to match exactly | portal pins, Portals layer, profile portal chips |
| home | `home` (legacy name; current Lucide `house` has different geometry. Match via `house` or inline the legacy path) | base pins, Structures layer |
| box | `box` | chest [OUT] |
| bed | `bed` | bed pins, Beds layer |
| skull | `skull` | tombstone, deaths |
| paw | `paw-print` | tame |
| ship | `ship` | ship, Ships & carts layer [OUT] |
| cart | **none (custom)**: two wheels + a trapezoid. Closest: `shopping-cart` | cart [OUT] |
| shield | `shield` | wards [OUT] |
| sign | `signpost` | sign |
| flame | `flame` | boss altar, boss events |
| coins | `coins` | trader |
| arch | **none found**: `M4 22V10a8 8 0 0 1 16 0v12`, `M2 22h20`, `M9 22v-6a3 3 0 0 1 6 0v6` (an arched gateway). Closest Lucide candidates: `door-closed` or `landmark`. [AMBIG] Proposal: keep it inline | crypt, burial, Locations layer |
| mountain | `mountain` | frost cave, Biomes & terrain layer |
| search | `search` | search box |
| layers | `layers` | Layers button |
| plus / minus | `plus` / `minus` | zoom |
| check | `check` | checkbox |
| copy | `copy` | Copy code, Copy |
| reset | `rotate-ccw` | Reset view |
| link | `link` | defined in `card.ic.link`, never rendered |
| x | `x` | close |
| chevL / chevR / chevU / chevD | `chevron-left` / `chevron-right` / `chevron-up` / `chevron-down` | panel collapse and expand, switcher |
| sun / moon | `sun` / `moon` | theme toggle, empty state (moon), time pill [OUT] |
| map | `map` | map-updated pill |
| menu | `menu` | mobile top bar |
| login / logout | `log-in` / `log-out` | activity join and leave |
| alert | `triangle-alert` (formerly `alert-triangle`) | stale banner |
| power | `power` | offline banner, server events |

---

## 5. Interactions (DCLogic)

### 5.1 Clustering (`proj`, `:1250-1279`)

- **Visible set**: markers whose layer is on **and** (fog off **or** `m.explored`). Note that fog hides *all* marker kinds in unexplored zones, not only locations.
- **Culling**: projected points outside `[-40, W+40] × [-40, H+40]` are dropped.
- **Greedy single pass** in marker-array order. Each point (unless it is the selected marker) joins the **first** existing group that is not locked and whose **running centroid** is `< 26 px` away (screen space). The centroid updates as a running mean. The selected marker forms a locked singleton group that nothing joins.
- It is recomputed on every render, at **every zoom**. There is no zoom threshold and no "disable at max zoom"; points separate naturally as the zoom grows. The `clustering` prop turns it off.
- **Cluster bubble**:
  - size `s = 34 + min(12, n)` px (35–46);
  - `background:#26231f; border:2px solid #f5ead8; box-shadow:0 0 0 5px rgba(38,35,31,.25), 0 3px 8px rgba(0,0,0,.35)`;
  - count text 13 px, 700, `#f5ead8`, tabular;
  - tooltip "`{n} markers · {up to 3 distinct kickers, ', '}`".
- **Clicking a cluster**: `zoomAt(g.x, g.y, 2.4)`, which multiplies the zoom by 2.4 about the cluster centre (+1.26 Leaflet zoom levels, §7.3). It does **not** spiderfy or fit bounds. A click is ignored if the pointer moved more than 4 px.
- Clusters mix kinds across layers.
- Leaflet note: `Leaflet.markercluster` uses a grid-based, per-zoom precomputed algorithm with a default `maxClusterRadius` of 80 and zoom-to-bounds on click. To match, set `maxClusterRadius: 26`, `zoomToBoundsOnClick: false` with a custom handler that zooms in by about 1.26 levels at the cluster, `showCoverageOnHover: false`, `spiderfyOnMaxZoom: true`, and a custom `iconCreateFunction`. Alternatively, a small custom clusterer that reruns on `zoomend` and `moveend` (the marker counts are small, in the hundreds). **Keep the selected marker out of clusters.**

### 5.2 Search (`:1368-1380`)

- The query is `q.toLowerCase().trim()`. It runs only when the query is non-empty and the map is ready.
- **Searchable types**: `portal`, `sign`, `tame`, `base`, `altar` and `trader` only. Not beds, tombstones, dungeons, ships, wards or chests.
- Markers in unexplored zones are skipped when fog is on.
- **Match**: `m.terms.some(t => t && t.includes(q))`, a case-insensitive **substring** match on pre-lowercased terms:
  - portal: `[tag]`
  - base: `[builder, others, title].join(' ').toLowerCase().split(/[ ,]+/)`, i.e. individual words of the builder names, co-builder names and title
  - tame: `[name, species]`
  - sign: `[text]`
  - altar: `[boss name]`
  - trader: `['trader', name]`
- **Ranking**: none. Results come in marker-array order, **capped at 8**.
- **Result title**: the sign text for signs, otherwise the title.
- **Sub-lines**:
  - portal paired hub: "Portal · Skovheim hall"; paired: "Portal · {biome}"; unpaired: "Portal · unpaired · {biome}"
  - base: "Base by {builder} · {pieces} pieces"
  - tame: "{species} · {place}"
  - everything else: "{Kicker} · {biome}"
- **Select** (on `mousedown`, with `preventDefault` so blur doesn't eat it): `centerOn(m, 5)` (design zoom 5 ≈ Leaflet 4.32), select the marker (which opens the popover), close the results.
- The spec's search scope: "portal tags, tame names, base names and builders, sign text, boss names". The design also matches **tame species** and **trader names**. Proposal: keep both, since they are cheap.
- MVP proposals:
  - Rank in this order: exact match, then prefix match, then substring; within each, portals, bases, tames, signs, altars, traders.
  - Replace biome in the sub-lines with something available: portal paired → "Portal · paired"; unpaired → "Portal · unpaired"; base → "Base by {builder} · {pieces} pieces"; tame → "{species} · near {base}" or just "{species}"; altar → "Boss altar · Defeated" or "Boss altar · Not yet defeated"; trader → "Trader"; sign → "Sign".
  - Use whole base names and builder names as terms (not just split words), so "halvor's" matches.

### 5.3 Layer toggles

- Toggling a layer flips `layers[k]` and re-projects.
- `portalLinks` is a sub-toggle, effective only when `portals` is on: lines are drawn iff `layers.portals && layers.portalLinks`.
- `biomes` off applies the greyscale filter (§3.13).
- The Layers badge counts the enabled main layers.
- Opening the layers panel is closed by: focusing search, opening join, selecting a marker, or clicking the map.
- The timeline filters (`tl`, `tlWho`) are [OUT].

### 5.4 Portal lines and "Jump to partner"

- **Pairs**: each pair is drawn once as a straight segment from end A to end B, in screen space (a div rotated by `atan2`; in Leaflet use an `L.polyline` in a pane below the markers).
- **Style**:
  - normal: `2px dashed #9cc4e0`, opacity .6;
  - if either end is selected: `3px solid #7fb2d6`, opacity 1;
  - if another marker is selected: opacity .25.

  These are fixed colours, not tokens.
- Lines are **not** filtered by fog or culling in the design. Both ends are drawn even when one pin is hidden. [AMBIG] Proposal: draw a line only when both ends are visible after layer and fog filtering.
- Z-order: below the pins, above the map and wards.
- **Jump to partner** (`jump`, `:1460`): `centerOn(partner, max(3, currentZoom))` (design zoom; ≈ Leaflet max(3.58, current)), then select the partner, so its card replaces the current one.
- `partnerId` comes from `marker.pair` (the backend's partner id, e.g. `portal-7`). Marker ids are only stable within one snapshot. On a snapshot refresh, drop the selection if its id no longer matches the same kind at the same position.

### 5.5 Fog of war (`finish`, `:1133-1147`)

The fog is painted into the world image itself:

1. Build a `ZN×ZN` (330×330) mask canvas with explored zones as opaque white. The row is flipped (`(ZN-1-z)`) because world +z is up.
2. Fog canvas (1024×1024 world pixels):
   - fill `#cfbe9c`;
   - add per-pixel grey noise `±9` (`(Math.random()-.5)*18` on RGB);
   - add diagonal hatching: stroke `rgba(120,98,66,.12)`, `lineWidth 1`, lines from `(x,0)` to `(x+N,N)` every **9 px** (45°, top-left to bottom-right).
3. Punch out the explored areas: `globalCompositeOperation='destination-out'`, `filter='blur(3px)'`, draw the mask scaled to world pixels. Three pixels on a 1024-px world image ≈ 61 m, so the soft edge is about one zone wide.
4. Composite the fog over the map with `source-atop`, so it covers only already-painted pixels: land and ocean inside the disc, not the transparent outside.

So the explored areas show the terrain, and the unexplored ones show a parchment texture. It also covers the ocean.

MVP: the spec says terrain tiles are never fogged and the fog is a **client-side mask** from `exploredZones`. Implement it as a Leaflet canvas overlay (e.g. a custom `L.GridLayer` that draws per tile, or one `L.ImageOverlay` from an offscreen canvas regenerated per snapshot) in a pane above the tiles and below the markers.
- Draw the texture per tile in screen space, so the hatching and noise don't scale with zoom.
- The blur radius should scale with zoom (about 0.9 zones).
- Clip to the world disc (radius 10 500 m).
- **Zone semantics** (backend): zone `(zx, zz)` covers `x ∈ [zx·64 − 32, zx·64 + 32)` (`locationZone`: `floor((x + 32)/64)`). This differs from the design's `floor(wx/64)`; use the backend convention.
- Pre-rasterise the explored set into a 1-px-per-zone mask canvas (about 330×330) and scale it with `imageSmoothingEnabled` plus a blur.
- The legend colour for "Unexplored" is `#cfbe9c`.

### 5.6 Zoom and pan (`zoomAt`, `:1155`; handlers, `:1449-1458`; wheel, `:1120`)

- **Zoom range**: `z ∈ [0.5, 12]` in design scale, where 1 = 1024 px across the world (§7.3 gives the Leaflet equivalents).
- **Wheel**: `factor = exp(-deltaY × 0.0016)` about the cursor; `passive:false` with `preventDefault`.
- **Buttons**: ×1.6 and ÷1.6 about the visible centre (x 900 or 720, y 470).
- **Reset**: `z = 0.84`, the world centred at x = 900 or 720, `oy = 20` (the world top 20 px below the frame top); it also clears the selection. The default view is `{z:.84, ox:470, oy:20}`.
- **Pan**: drag with pointer button 0. Movement is tracked in `Math.abs(dx)+Math.abs(dy)`. The view starts moving only once moved > 3; a click is suppressed when moved > 4. **No pan limits exist in the design.** Proposal: Leaflet `maxBounds` = the world square plus about 20% padding, with `maxBoundsViscosity: 0.8`.
- **Map click** (not after a drag): clears the selection and closes layers.
- **Cursor**: `grab`, or `grabbing` while dragging.
- **centerOn(m, z)**: puts the marker at the visible centre (x 900/720, y 470) at design zoom z. It is used with z = 4 (activity and timeline "show on map"), 5 (search), 3 ("Show altar"), and max(3, z) (jump).

### 5.7 Scale bar (`:1426-1427`)

`MPP = 2·10500/1024 ≈ 20.51` m per world pixel at z = 1.

`target = 110 / z × MPP` metres (the metres covered by 110 screen px). `nice` = the first of `[50,100,200,250,500,1000,2000,2500,5000]` that is `≥ target × 0.7`, else 5000. The bar width is `round(nice / MPP × z)` px, with the label "`{nice/1000} km`" when ≥ 1000, else "`{nice} m`".

In Leaflet, use metres per screen pixel = `21000 / (256 · 2^zoom)`.

### 5.8 Cursor readout

Pointer moves are throttled with `requestAnimationFrame`, and pointer leave resets the readout. Coordinates are `X {fmtN(wx)} · Z {fmtN(wz)}`.

### 5.9 Popover placement (`:1323-1328`)

- Anchor at the pin centre `(x, y)`.
- `left = x + 26`; if `left + 312 > 1424` (viewport width − 16), use `x − 26 − 312`.
- `top = clamp(y − 60, 96, 900 − 440)`, i.e. `[96, viewportHeight − 440]`.
- It follows the pin on pan and zoom (re-projected every render).
- Closed by the close button, a map click, or `Esc` (proposal; not in the design).
- On mobile the card docks at the bottom (`left/right 12`, `bottom 28`) and the map is centred with the marker at y ≈ 300.

### 5.10 Side panel state

- `panelOpen` toggles between the full panel and the collapsed pill.
- The map-updated pill and the readout shift left accordingly (376 ↔ 190 / 16).
- The tab (`players` or `world`) persists while the panel is collapsed.
- The theme toggle is in the panel header.
- `serverMenu` toggles the switcher dropdown. Picking a server closes it. [AMBIG] The design does not close it on an outside click; proposal: add that.

### 5.11 Mobile sheet

There is no gesture logic in the design (§3.18). The implied snaps are peek (about 161 px), pulled (top 176) and full (top 54).

---

## 6. Copy and formatting

### 6.1 Relative time and durations (design samples; the formatters are proposals that reproduce them)

- **Map age** (pill): "Map updated **{N} min ago**", then " · next save in ~{M} min". Mobile: "Map {N} min ago · next ~{M} min". Proposals:
  - N < 1: "Map updated just now";
  - N ≥ 60: "Map updated {h} h {m} min ago", per the stale banner's "3 h 41 min ago".
- **Session length** (online list): compact `{h}h {mm}m` or `{m}m`, e.g. "1h 12m", "2h 40m", "38m", "5m". The timeline uses a zero-padded "3h 05m" and the list does not ("1h 12m"). Proposal: `{h}h {m}m` without padding in lists; with hours ≥ 24, "1d 3h".
- **Activity time** (relative): "5 min", "24 min", "38 min", "**1 h 12 m**" (spaced, inconsistent with the session format), "save 14:20", "day 198". Proposal: "{m} min" under 60, "{h} h {m} m" under 24 h, else "{Mon d}" (e.g. "28 Sep").
- **Recent**: "last seen {N} min ago" (e.g. "last seen 24 min ago"). Proposal: "last seen {h} h ago" or "last seen yesterday" beyond that.
- **Uptime**: "3d 6h" (`{d}d {h}h`, or `{h}h {m}m` under a day).
- **Offline banner**: "last seen online today 03:12 (11 h ago)" → `last seen online {today|yesterday|D Mon} {HH:MM} ({N} h ago)`.
- **Stale banner**: "Map last updated 3 h 41 min ago · saves usually every ~20 min".
- **Join code note**: "Issued at today’s 06:00 restart."
- **Snapshot line**: "Snapshot from the 14:32 autosave".
- Clock times are 24-hour `HH:MM`, in the viewer's local time zone. API times are RFC 3339 UTC.
- Timeline session bars and hours are [OUT].

### 6.2 Numbers

- `fmtN` (`:1047`): `(n<0?'−':'') + Math.abs(Math.round(n)).toLocaleString('en-US')`. It rounds, uses a comma thousands separator, and a **U+2212 minus sign**. Example: `X −1,234 · Z 3,050`.
- Piece counts: `toLocaleString('en-US')` → "2,184 pieces".
- Tiles: "389 of 1,024 tiles".
- Players: "4/10" (card, dialog status line, switcher), "4 / 10" (mobile pill), "Online · 4" (tab), "4 online" (collapsed pill), "—/10" (offline mini-panel). Offline count: proposal "—/{max}".
- Explored: "{pct}%" (the design uses an integer; the API gives 1 decimal, e.g. 12.4%).
- Distance: `(metres/1000).toFixed(1) + ' km'` plus a compass word from `dir8` (`:1048`): `['north','north-east','east','south-east','south','south-west','west','north-west'][round(((atan2(dx,dz)·180/π)+360)%360/45)%8]`, where dx = east and dz = north.
- Stars: `'★'.repeat(n) + '  (' + n + '-star)'` or "No stars" [OUT].
- Join code: the display groups 3+3 with a space ("318 742"); copying gives the raw digits.
- Boss count: "4 / 8" (tile), "4 of 8 bosses" (switcher).
- Roman numerals I–VIII for the boss discs.
- Scale: "500 m", "1 km", "2.5 km".

### 6.3 Charting copy

"Charting the world for the first time" · "{pct}% · {done} of {total} tiles" · "~{n} min left" · "Tiles sharpen as they finish. Markers appear once the first pass is done. The online list already works." The desktop inline pill reads "Charting Example from the world save…" (use "Charting {serverName} from the world save…").

### 6.4 Empty states and other copy

- Search: "Try" (hints), and "Nothing matches “{q}” in the last save."
- Nobody online: "The longhouse is quiet" / "Nobody’s online right now. {Name} was the last to leave, {dur} ago. The fire’s still warm."
- Offline: "Server is resting" / "Nobody can join until it’s back. The map shows the final save before shutdown ({HH:MM})."
- Players tab intro: "Live from the server. Player positions aren’t tracked."
- Mobile sheet sub-line: "{serverName} · live · positions aren’t tracked".
- World tab footnote: "Bosses come from the world’s progress keys. Exploration comes from the zones the server has generated."
- World rules footnote: "Orange = changed from Normal. Read from the world’s modifier keys."
- Other [OUT] empty states: "No bases yet.", "None yet.", "Nothing matches these filters."
- [AMBIG, proposals] Empty recent list: hide the "Recently online" label. Empty activity: "No server activity yet." Empty layer: no rows hidden. A search with no snapshot: disable the input with the placeholder "Waiting for the first save…".

---

## 7. Sample data and API mapping

### 7.1 Sample data shape (design)

- **Markers** `M[]` (`buildData`, `:1161-1239`): `{ id: '{type}-{n}', type, layer, wx, wz, px, py, biome, terms[], title, explored, … }`, with type-specific fields:
  - base `{builder, pieces, others}`
  - portal `{tag, paired, hub?, partnerId}`
  - bed `{owner}`
  - tomb `{player, after, prev, count, fresh?}`
  - tame `{name, species, lvl, place}`
  - ship/cart `{near}`
  - sign `{text, by}`
  - trader `{sells}`
  - altar `{boss, defeated}`
  - ward `{owner, place}`
  - crypt, cave and burial have a title only.
- **Sample content**:
  - 6 bases (Skovheim 2184 pieces by Frøya, Lox Ranch 612, Stilt House 344, Halvor's Harbour 488, Mountain Lodge 521, Mistgate Outpost 203);
  - 14 portal pairs from a 14-portal hub (tags home, swamp1, swamp2, mistlands-camp, lox, trader, elder, bonemass, moder, yagluth, harbour, crypts, silver, tar-pit) plus 1 unpaired "copper";
  - 6 beds, 3 tombs, 12 tames (wolves and lox), 2 ships, 2 carts, 6 signs, 3 traders, 8 altars (the first 4 defeated), 3 crypts, 2 caves, 3 burials, and 6 wards.
- **Explored zones**: a `330×330` `Uint8Array` indexed `[z·ZN + x]`, with `x = floor(wx/64) + 165`. The explored % counts zones whose centre is within 10 000 m.
- **Server card**: hard-coded strings (Players 4/10, Version 1.0.16, Uptime 3d 6h, code 318 742 issued 06:00, address `play.example.net:2456`, Discord `#example`, day 214, bosses 4/8, preset "Custom · based on Normal").
- **Players**: `[['Johnny','1h 12m'],['Alina','38m'],['Halvor','5m'],['Frøya','2h 40m']]`. Recent: Bjørn, "last seen 24 min ago".
- **Activity**: §3.8. Timeline, profiles, sessions, weather and TOD are [OUT].

### 7.2 Field mapping to the API

| Design element | API source | Notes |
|---|---|---|
| serverName | `Card.name` | |
| status dot and label | `Card.status` | §3.3 |
| Players n/max | `Card.players`, `Card.maxPlayers` | |
| Version | `Card.version` | optional |
| Uptime | `Card.upSince` | optional |
| Online list | `Card.online[] {name, platform, platformId, since}` | platform not shown in the design |
| Recently online | `Card.recent[] {name, …, until, seconds}` | dedupe (§3.7) |
| Recent activity | `Card.activity[] {type, at, name?, …}` | log events only |
| Join code | `Card.joinCode`, `Card.joinCodeAt` | |
| Address | `Card.address` | |
| Crossplay | `Card.crossplay` | "crossplay on/off" |
| Password hint | `Card.discordHint` | |
| In-game day | `Card.world.day` | |
| Bosses | `Card.world.bosses[] {key, name, defeated}` | the 8th is "Writhan", not "Kall Fimbulbringer" |
| World rules | `Card.world.modifiers`, `Card.world.flags` | §3.10 |
| Explored % | `Card.world.exploredPct` | |
| Map updated / next save | `Card.world.savedAt`, `Card.world.saveIntervalSec?` | `readAt` also available |
| Charting | `Card.tiles {state, done, total, key?}` | |
| Tile URL | `/tiles/{id}/{tiles.key}/{z}/{x}/{y}.png` | only when `state === 'complete'` |
| Server switcher | `GET /api/servers` → `ServerSummary {id, name, status, players, maxPlayers}` | no day or bosses |
| Portals | `snapshot.markers[kind=portal] {id, x, z, label (tag), pair?}` | |
| Beds | `markers[kind=bed] {owner}` | |
| Tombstones | `markers[kind=tombstone] {owner}` | |
| Signs | `markers[kind=sign] {label (text)}` | |
| Tames | `markers[kind=tame] {species, label (TamedName)}` | |
| Bases | `snapshot.bases[] {id, name, x, z, radius, pieces, builders[{id, name?, pieces}]}` | |
| Altars, traders, dungeons | `snapshot.locations[] {id, kind, type, label, x, y, z}` | already filtered to explored zones |
| Fog | drawn into the server's tiles (`snapshot.fogKey` names them); `snapshot.explored` is the 12 m mask, for the cursor readout | markers, locations and bases already filtered to it (fog spec 2026-10-01) |
| Player id → name | `snapshot.players[] {id, name}` | from beds and tombstones; used by bases' builders |
| Per-player base count (mobile) | `bases[].builders[0].name === online.name` | [AMBIG] the log name and the character name should match, but unverified |

### 7.3 Coordinate mapping (design → Leaflet)

- Design: `toP(wx, wz) = [(wx/R + 1)/2 · N, (1 − wz/R)/2 · N]`, with R = 10 500 and N = 1024. North (+z) is up and east (+x) is right. The tiles match (`tiles.go:301-305`): the level-5 image is 8192 px spanning `[−10500, 10500]` on both axes, with +z at the top.
- Leaflet `CRS.Simple` at zoom 0 = 256 px for the world: `latLng = [−(10500 − z)/21000·256, (x + 10500)/21000·256]`. (CRS.Simple has y pointing up in lat, so negate the lat or use a custom transformation `L.Transformation(256/21000, 128, -256/21000, 128)` with `[z, x]` input.)
- **Zoom equivalence**: design `z` = 1 is 1024 px = Leaflet zoom 2. Leaflet zoom = `2 + log2(z)`:
  - design range 0.5–12 → Leaflet **1 – 5.58**, so set `maxZoom ≈ 5.5–6` (native tiles to 5, overzoom beyond);
  - default 0.84 → 1.75;
  - base labels at z ≥ 0.7 → Leaflet ≥ 1.49;
  - `centerOn` zooms 3, 4 and 5 → 3.58, 4 and 4.32;
  - cluster click ×2.4 → +1.26;
  - zoom buttons ×1.6 → +0.68 (use `zoomDelta: 0.68` or keep Leaflet's default of 1). [AMBIG]
  - Enable `zoomSnap: 0` or 0.25 for smooth wheel zoom.

### 7.4 Shown in the design, not provided by the API

| Design feature | Status in MVP |
|---|---|
| Player positions | Never shown (spec). The design has none either. |
| Time of day, clock, day progress, night map tint, weather per biome (time pill and dropdown) | **Omit** [OUT, section 08] |
| Player profiles (playtime, 7-day chart, first seen, deaths, portals placed, tames named) | **Omit** [OUT, section 08]. Remove "Profile →" |
| Activity timeline, session bars, save-diff events (new tombstone, new portal, new tame, base growth, raids, boss defeated) | **Omit** [OUT, section 05]. Remove "Full timeline →" and save-derived activity rows |
| Deaths count | Omit |
| Biome of a marker, cursor biome, portal partner biome, "Next up … in the Plains" | Omit, or use a static boss → biome map |
| Tombstone freshness ("New since last check", "1 new since 14:20"), "Appeared after save", items inside | Omit (needs diffing) |
| Tame level (stars), tame "Near" place | Omit level; compute "Near" client-side from bases (optional) |
| Sign "Placed by" | Omit |
| Base "Wards: 1 active" | Omit |
| Wards (layer and circles), chests (contents), ships, carts (Ships & carts layer) | Omit layers (not extracted) |
| Server switcher sub-line "Day 214 · 4 of 8 bosses" for other servers | Fallback to status text |
| Archived servers ("Example (2024)", read-only map) | Omit |
| "Admins add servers in Settings" | Replace copy (no Settings) |
| Charting ETA "~2 min left" and low-res preview | Client-side estimate / plain placeholder |
| World preset name | Derived ("Normal" / "Custom · based on Normal") |
| Search hints (sample names) | Derive from data or omit |
| Join code for non-crossplay servers | N/A; address-only variant (undesigned) |
| Theme persistence | Add localStorage (not in design) |

---

## 8. Layer mapping (backend kinds → design kinds → layers)

Grounded in `internal/extract/extract.go` (`Add`, `Finish`) and `tables.go`.

**`snapshot.markers[].kind`** (from ZDOs):

| backend kind | produced when | fields | design type | layer (default) | disc |
|---|---|---|---|---|---|
| `portal` | prefab name starts with `portal` | `label` = tag, `pair` = partner id when exactly 2 share the tag | portal | portals (on), with lines under portalLinks (on) | COLD 28; ember ring if no `pair` |
| `bed` | prefab `bed` or `piece_bed*` | `owner` = ownerName | bed | beds (**off**) | INK 26 |
| `tombstone` | `Player_tombstone` | `owner` | tomb | tombstones (on) | EMBER 28 |
| `sign` | prefab `sign` | `label` = text | sign | signs (**off**) | INK 26 |
| `tame` | TamedName set or `tamed == 1`, excluding `Skeleton_Friendly` | `species` = prefab name, `label` = TamedName (may be empty) | tame | tames (on) | INK 26 |

**`snapshot.locations[].kind`** (from the world's location list, filtered server-side to explored zones):

| backend kind | `type` (prefab) → `label` | design type / icon | layer |
|---|---|---|---|
| `boss_altar` | Eikthyrnir → Eikthyr, GDKing → The Elder, Bonemass → Bonemass, Dragonqueen → Moder, GoblinKing → Yagluth, Mistlands_DvergrBossEntrance1 → The Queen, FaderLocation → Fader, DN_Bossroom → Writhan | altar / flame; SAGE disc if defeated, else INK; 30 px | locations (on) |
| `trader` | Vendor_BlackForest → Haldor, Hildir_camp → Hildir, BogWitch_Camp → Bog Witch | trader / coins | locations |
| `dungeon` | SunkenCrypt4 → Sunken crypt | crypt / arch | locations |
| `dungeon` | Crypt2, Crypt3, Crypt4 → Burial chambers | burial / arch | locations |
| `dungeon` | MountainCave02 → Frost cave | cave / mountain | locations |
| `dungeon` | TrollCave02 → Troll cave, BearCave → Bear cave, Hildir_cave → Howling cavern | cave-like / mountain (proposal) | locations |
| `dungeon` | Hildir_crypt → Smouldering tomb | crypt-like / arch (proposal) | locations |
| `dungeon` | Hildir_plainsfortress → Sealed tower, GoblinCamp2 → Fuling village | no design type / arch (the design's default location icon) [AMBIG] | locations |

For dungeons, kicker = `label` and title = `label`, the design's pattern for crypts, e.g. "Sunken crypt" / "Sunken crypt". [AMBIG] Proposal: kicker "Dungeon", title = `label`, to avoid the duplication.

The spec says dungeons are "zoom-gated". The design has no zoom gating (it clusters instead). [AMBIG] Proposal: hide `dungeon` locations below Leaflet zoom 3 (design z ≈ 2), and keep altars and traders at all zooms.

**`snapshot.bases[]`** (not a marker kind) → design type `base` / home, layer **structures** (on), CREAM 36 px disc with an INK border, and a name label at Leaflet zoom ≥ 1.5.

**Layers with no backend source** (drop them in the MVP): `vehicles` (ship, cart), `wards` (ward). There is also no `chest` type. `biomes` is a map-filter toggle, not a data layer.

**Counts** for the layer rows:

| layer | count |
|---|---|
| structures | `bases.length` |
| portals | markers with `kind==='portal'` |
| portal links | "`{n} pairs`", where n = portals with `pair` ÷ 2 |
| beds | markers with `kind==='bed'` |
| tombstones | markers with `kind==='tombstone'` |
| tames | markers with `kind==='tame'` |
| signs | markers with `kind==='sign'` |
| locations | `locations.length` |
| biomes | the design shows the constant `9` |

[AMBIG] Should counts include fogged-out items? Player-placed markers are almost always in explored zones, and the backend already filters locations, so the difference is negligible. Proposal: count what is visible.
