# Farsight Plan 4a: Central App Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the central `farsight` binary. It receives snapshots and events from the agents, stores them, turns events into live state and player sessions, renders each world's map tiles in the background, gates everything behind per-server passphrases, and serves the JSON API and tiles that the web UI (Plan 4b) consumes.

**Architecture:**
- A single Go binary, `cmd/farsight`, backed by SQLite (pure-Go `modernc.org/sqlite`) and a tile cache on disk.
- `internal/config` loads JSON config. `internal/store` owns the schema and queries. `internal/live` applies events idempotently and closes orphaned sessions. `internal/server` holds the HTTP handlers: ingest (bearer), auth (HMAC cookie), web API and tiles.
- `internal/tileset` runs one background render at a time on top of `worldgen` and `tiles`.
- The standard library `net/http` router handles method and path patterns.

**Tech Stack:** Go standard library plus `modernc.org/sqlite` and `golang.org/x/crypto/bcrypt`.

**Spec:** `docs/superpowers/specs/2026-09-29-farsight-atlas-mvp-design.md`: the sections "farsight (central)", "Event contract rules for the central app" and "Snapshot". Plan 4b builds the SvelteKit UI against the API contract fixed here.

## Global Constraints

- Go module `github.com/jumpingmushroom/farsight`. The only allowed new dependencies are `modernc.org/sqlite` (no cgo) and `golang.org/x/crypto`.
- Ingest contract, unchanged from Plans 1 and 3:
  - `POST /ingest/{server}/snapshot`: gzip JSON `extract.Snapshot`.
  - `POST /ingest/{server}/events`: gzip JSON `{"events":[logwatch.Event…]}`.
  - Both use `Authorization: Bearer <token>`, bcrypt-compared against the server's `agentTokenHash`.
  - The events endpoint returns 2xx for a well-formed batch and skips invalid events individually. Dedupe is on `(serverId, id)`, and it must be cheap, because a restarted agent replays everything since pod start.
- Event contract rules, copied from the spec:
  - Close every open session for a server on `server_starting`, `server_boot` or `server_stopped`, and after 3 min without a heartbeat. The end time is the server's last heartbeat or event time.
  - The pairing key is `(serverId, platformId, name, since == join.at)`.
  - Tolerate a leave without its join, and a join without its leave.
  - `seconds` is omitted when 0; `players` is present when 0.
- Tiles live under `{dataDir}/tiles/` and use `tiles.SetDir(root, seed, genVersion)`. Refuse `genVersion > worldgen.MaxGenVersion`. Garbage-collect sets from other `RenderVersion`s once the current one is complete. Run at most one render at a time, with `Workers = max(1, GOMAXPROCS-1)`.
- Auth:
  - Each server has a shared passphrase (bcrypt hash in config).
  - `POST /api/unlock` sets an HMAC-SHA256-signed, HttpOnly, SameSite=Lax cookie listing unlocked server ids, valid for 1 year. It's Secure unless `cookieSecure:false`.
  - Failed unlocks are rate-limited per client IP.
  - Every `/api/servers…` and `/tiles/…` request for a server that isn't unlocked returns 404, not 403, so server ids don't leak.
- Config: a JSON file (`-config`, default `/etc/farsight/farsight.json`) plus the env var `FARSIGHT_COOKIE_KEY` (at least 32 bytes, as raw text or base64). This amends the spec's "farsight.yaml" (see Task 8).
- Secrets (tokens, passphrases, cookie key) are never logged.
- Commits: `feat(pkg): …`, each ending with a blank line and then `Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T`.

## API contract (for Plan 4b)

All times are RFC 3339 UTC strings. Absent optional fields are omitted.

```
GET  /api/servers                         -> {"servers":[ServerSummary]}         (unlocked only, config order)
     ServerSummary = {id, name, status, players, maxPlayers}
GET  /api/servers/{id}                    -> Card
     Card = {
       id, name, crossplay, address?, discordHint?, maxPlayers,
       status: "online"|"starting"|"restarting"|"offline"|"unknown",
       version?, networkVersion?, upSince?, lastHeartbeat?,
       players, joinCode?, joinCodeAt?,
       online:   [{name, platform, platformId, since}],                    (since asc)
       recent:   [{name, platform, platformId, since, until, seconds}],    (ended in last 72 h, until desc, max 50)
       activity: [{type, at, name?, platform?, code?, players?, seconds?, version?}], (last 50 non-heartbeat/players_now events, at desc)
       world?: {name, seedName, day, bosses:[{key,name,defeated}], modifiers:{}, flags:[],
                exploredPct, savedAt, readAt, saveIntervalSec?},
       tiles:  {state:"none"|"queued"|"rendering"|"complete"|"refused", done, total, key?}
     }
GET  /api/servers/{id}/snapshot           -> {savedAt, exploredZones, markers, locations, bases, players}
                                             (locations filtered to explored zones; 404 if no snapshot)
GET  /tiles/{id}/{key}/{z}/{x}/{y}.png    -> PNG (only when key is the server's current complete set;
                                             Cache-Control: public, max-age=31536000, immutable)
POST /api/unlock {server, passphrase}     -> 204 + Set-Cookie | 401 {"error":"wrong passphrase"} | 429 {"error":"too many attempts"}
GET  /healthz                             -> 200 "ok"
```

Status rules, applied at read time:
- `offline` if there has been no heartbeat for 3 min.
- Otherwise `restarting` after `server_stopped`, `starting` after `server_starting` or `server_boot`, and `online` after `server_ready`.
- `unknown` before any event has been seen.

`exploredPct` = explored zones ÷ zones whose centre lies within 10 500 m (a constant computed once), × 100, rounded to 1 decimal place.

`saveIntervalSec` = the median gap between the last up to 10 `world_saved` events. It's omitted when there are fewer than 3.

## File Structure

```
internal/config/config.go         # Load(path) (*Config, error); validation; cookie key from env
internal/config/config_test.go
internal/store/store.go           # Open(dataDir) (*Store, error); schema/migrations
internal/store/snapshots.go       # PutSnapshot, LatestSnapshot, PruneSnapshots
internal/store/events.go          # InsertEventIfNew, RecentActivity, SaveTimes, PruneEvents
internal/store/sessions.go        # OpenSession, CloseSession, CloseAllOpen, Online, Recent
internal/store/live.go            # GetLive, PutLive
internal/store/store_test.go
internal/live/apply.go            # Applier.Apply(serverID, []logwatch.Event) (applied, skipped int, err); SweepStale(now)
internal/live/apply_test.go
internal/auth/cookie.go           # Codec{Key}: Encode/Decode unlocked set; Limiter
internal/auth/auth_test.go
internal/tileset/manager.go       # Manager: Ensure(seed, gen) state; Status(seed, gen); GC; injectable render func
internal/tileset/manager_test.go
internal/server/server.go         # New(deps) http.Handler; routes; middleware
internal/server/ingest.go         # snapshot + events handlers
internal/server/api.go            # servers list, card, snapshot, unlock
internal/server/tiles.go          # tile file serving
internal/server/server_test.go    # handler tests with httptest
internal/server/e2e_test.go       # agent -> farsight -> API end to end
internal/worldgen/sampler.go      # per-worker corner-biome cache (render speed-up)
cmd/farsight/main.go              # "serve" and "hash" subcommands
```

---

### Task 1: Config and the `farsight hash` subcommand

**Files:** Create `internal/config/config.go`, `internal/config/config_test.go`, `cmd/farsight/main.go` (with only the `hash` subcommand for now and a `serve` stub that exits 2 with "not implemented"). Run `go get golang.org/x/crypto@latest`.

**Interfaces:**
```go
type Server struct {
    ID             string `json:"id"`             // ^[a-z0-9][a-z0-9-]{0,40}$
    Name           string `json:"name"`
    Address        string `json:"address,omitempty"`
    Crossplay      bool   `json:"crossplay"`
    DiscordHint    string `json:"discordHint,omitempty"`
    MaxPlayers     int    `json:"maxPlayers"`     // default 10
    PassphraseHash string `json:"passphraseHash"` // bcrypt
    AgentTokenHash string `json:"agentTokenHash"` // bcrypt
}
type Config struct {
    Listen       string   `json:"listen"`       // default ":8080"
    DataDir      string   `json:"dataDir"`      // default "/data"
    CookieSecure *bool    `json:"cookieSecure"` // default true
    TrustProxy   bool     `json:"trustProxy"`   // use the rightmost X-Forwarded-For entry as client IP
    Servers      []Server `json:"servers"`
    CookieKey    []byte   `json:"-"`            // from FARSIGHT_COOKIE_KEY
}
func Load(path string, getenv func(string) string) (*Config, error)
func (c *Config) Server(id string) (*Server, bool)
func (c *Config) Secure() bool
```
- **Validation:** reject unknown JSON fields (`DisallowUnknownFields`). There must be at least one server. Ids must be unique and match the regex. Both hashes must be bcrypt, i.e. `bcrypt.Cost(hash)` succeeds. The cookie key must be at least 32 bytes after decoding (try base64 std or url, otherwise use the raw bytes).
- `farsight hash` reads one line from stdin, trims `\r\n`, rejects empty input, and prints `bcrypt.GenerateFromPassword(cost 12)`.

- [ ] **Step 1: Write the failing tests** — `internal/config/config_test.go`

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func hash(t *testing.T, s string) string {
	h, err := bcrypt.GenerateFromPassword([]byte(s), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(h)
}

func write(t *testing.T, body string) string {
	p := filepath.Join(t.TempDir(), "farsight.json")
	os.WriteFile(p, []byte(body), 0o600)
	return p
}

const key = "0123456789abcdef0123456789abcdef-long-enough"

func env(k string) string {
	if k == "FARSIGHT_COOKIE_KEY" {
		return key
	}
	return ""
}

func TestLoadDefaultsAndLookup(t *testing.T) {
	p := write(t, `{"servers":[{"id":"mulevikings","name":"Mulevikings","crossplay":true,
	  "passphraseHash":"`+hash(t, "pw")+`","agentTokenHash":"`+hash(t, "tok")+`"}]}`)
	c, err := Load(p, env)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":8080" || c.DataDir != "/data" || !c.Secure() || string(c.CookieKey) != key {
		t.Fatalf("defaults = %+v", c)
	}
	s, ok := c.Server("mulevikings")
	if !ok || s.MaxPlayers != 10 || !s.Crossplay {
		t.Fatalf("server = %+v %v", s, ok)
	}
	if _, ok := c.Server("nope"); ok {
		t.Fatal("unknown server found")
	}
}

func TestLoadRejects(t *testing.T) {
	good := `"passphraseHash":"` + hash(t, "pw") + `","agentTokenHash":"` + hash(t, "tok") + `"`
	cases := map[string]string{
		"no servers":    `{"servers":[]}`,
		"bad id":        `{"servers":[{"id":"Bad Id","name":"x",` + good + `}]}`,
		"duplicate id":  `{"servers":[{"id":"a","name":"x",` + good + `},{"id":"a","name":"y",` + good + `}]}`,
		"plain hash":    `{"servers":[{"id":"a","name":"x","passphraseHash":"pw","agentTokenHash":"tok"}]}`,
		"unknown field": `{"servers":[{"id":"a","name":"x",` + good + `}],"extra":1}`,
	}
	for name, body := range cases {
		if _, err := Load(write(t, body), env); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	short := func(string) string { return "short" }
	if _, err := Load(write(t, `{"servers":[{"id":"a","name":"x",`+good+`}]}`), short); err == nil || !strings.Contains(err.Error(), "FARSIGHT_COOKIE_KEY") {
		t.Errorf("short cookie key: err = %v", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails.** Then implement `config.go` and `cmd/farsight/main.go` (`hash`, plus the `serve` stub).
- [ ] **Step 3: Run** `gofmt -w . && go vet ./... && go test ./internal/config/ && echo -n secret | go run ./cmd/farsight hash`. Expected: `ok`, then a `$2a$12$…` hash.
- [ ] **Step 4: Commit** (`feat(config): JSON config with bcrypt hashes; farsight hash`, with the trailer).

---

### Task 2: SQLite store

**Files:** Create `internal/store/{store,snapshots,events,sessions,live}.go` and `internal/store/store_test.go`. Run `go get modernc.org/sqlite@latest`.

**Interfaces:**
```go
type Store struct{ /* *sql.DB */ }
func Open(path string) (*Store, error) // path "" => in-memory for tests; WAL, busy_timeout 5000, foreign_keys on; runs migrations
func (s *Store) Close() error

// snapshots — JSON blob stored gzip-compressed
func (s *Store) PutSnapshot(ctx, serverID, saveID string, savedAt, readAt time.Time, blob []byte) (inserted bool, err error) // idempotent on (server_id, save_id)
func (s *Store) LatestSnapshot(ctx, serverID string) (blob []byte, savedAt time.Time, ok bool, err error) // max saved_at
func (s *Store) PruneSnapshots(ctx, before time.Time) (int64, error) // never deletes a server's latest

// events — dedupe table + activity
func (s *Store) InsertEventIfNew(ctx, tx *sql.Tx, serverID string, e logwatch.Event) (bool, error) // INSERT OR IGNORE on PK(server_id,id)
func (s *Store) RecentActivity(ctx, serverID string, limit int) ([]logwatch.Event, error) // excludes heartbeat, players_now; at desc
func (s *Store) SaveTimes(ctx, serverID string, limit int) ([]time.Time, error)           // world_saved at desc
func (s *Store) PruneEvents(ctx, before time.Time) (int64, error)

// sessions
type Session struct{ ServerID, Name, Platform, PlatformID, Reason string; Since time.Time; Until *time.Time; Seconds int64 }
func (s *Store) OpenSession(ctx, tx, sess Session) error                 // ignore if (server,platformId,name,since) exists
func (s *Store) CloseSession(ctx, tx, sess Session) (found bool, err error) // sets until/seconds/reason on the matching open row
func (s *Store) InsertClosedSession(ctx, tx, sess Session) error          // leave without join
func (s *Store) CloseAllOpen(ctx, tx, serverID string, until time.Time, reason string) (int64, error) // seconds = max(0, until-since)
func (s *Store) Online(ctx, serverID string) ([]Session, error)           // until IS NULL, since asc
func (s *Store) Recent(ctx, serverID string, after time.Time, limit int) ([]Session, error) // until >= after, until desc
func (s *Store) ServersWithOpenSessions(ctx) ([]string, error)

// live state (one row per server)
type Live struct {
    ServerID, Status, Version, JoinCode string
    NetworkVersion, Players int
    LastHeartbeat, LastEventAt, UpSince, JoinCodeAt, StatusAt time.Time // zero = unknown
}
func (s *Store) GetLive(ctx, serverID string) (Live, bool, error)
func (s *Store) PutLive(ctx, tx, l Live) error
func (s *Store) Tx(ctx, fn func(*sql.Tx) error) error
```
- **Schema** (migration 1):
  - `snapshots(server_id, save_id, saved_at, read_at, received_at, blob, PRIMARY KEY(server_id, save_id))` with an index on `(server_id, saved_at)`.
  - `events(server_id, id, type, at, body, PRIMARY KEY(server_id, id)) WITHOUT ROWID` with an index on `(server_id, at)`.
  - `sessions(server_id, platform_id, name, since, platform, until, seconds, reason, UNIQUE(server_id, platform_id, name, since))` with an index on `(server_id, until)`.
  - `live(server_id PRIMARY KEY, …columns…)`.
  - `schema_version(v)`.
  - Times are stored as INTEGER Unix milliseconds UTC.
- A `nil` tx means the method runs on the DB directly.

- [ ] **Step 1: Write the failing tests** — `internal/store/store_test.go`. Cover:
  - snapshot put is idempotent, and `LatestSnapshot` returns the newest `savedAt` with the blob round-tripped through gzip;
  - pruning keeps the latest snapshot;
  - `InsertEventIfNew` returns true then false for the same `(server,id)`, and true for the same id on another server;
  - `RecentActivity` excludes heartbeats and orders by `at` desc;
  - open → close session pairing by the exact key; a close with no match returns `found=false`;
  - `CloseAllOpen` computes seconds and clamps them at 0 when `until < since`;
  - `Online`/`Recent` ordering;
  - live round trip;
  - `Open("")` works, and `Open(path)` persists across reopen.
- [ ] **Step 2: Run to verify it fails.** Then implement.
- [ ] **Step 3: Run** `gofmt -w . && go vet ./... && go test ./internal/store/ -v`. Expected: PASS.
- [ ] **Step 4: Commit** (`feat(store): SQLite store for snapshots, events, sessions and live state`, with the trailer).

---

### Task 3: Applying events and sweeping stale servers

**Files:** Create `internal/live/apply.go`, `internal/live/apply_test.go`.

**Interfaces:**
```go
type Applier struct{ Store *store.Store; Now func() time.Time }
func (a *Applier) Apply(ctx, serverID string, evs []logwatch.Event) (applied, skipped int, err error)
func (a *Applier) SweepStale(ctx) (closed int64, err error) // closes open sessions of servers whose LastHeartbeat is > 3 min old
const HeartbeatTimeout = 3 * time.Minute
func Status(l store.Live, now time.Time) string // read-time status per the API contract
```
- **Apply**, in one transaction per batch, in the order given:
  - An event is **invalid**, and is counted as `skipped`, if its id is empty, its type is unknown, `at` is zero, `at` is more than 10 min in the future relative to `Now()`, or `at` is older than `Now() - (live.EventRetention - 24h)` (13 days). `live.EventRetention` (14 days) is also serve's prune horizon, so an event whose dedupe row may already be pruned can never be re-applied by an agent replay.
  - If `InsertEventIfNew` returns false, the event is a duplicate: count it as skipped and do nothing else.
  - Otherwise track `lastSeen = max(live.LastHeartbeat, live.LastEventAt)` **before** this event, then act on the type:
    - `heartbeat`: `LastHeartbeat = max(LastHeartbeat, at)`.
    - `server_stopped`: `CloseAllOpen(until=at, reason "server_stopped")`; `Status="restarting"`.
    - `server_starting` and `server_boot`: `CloseAllOpen(until=min(at, lastSeen) if lastSeen non-zero else at, reason "server_lost")`; `Status="starting"`; `UpSince=at`. For boot, also set Version and NetworkVersion.
    - `server_ready`: `Status="online"`.
    - `players_now`: `Players=*e.Players`.
    - `join_code`: `JoinCode=e.Code`; `JoinCodeAt=at`.
    - `player_join`: `OpenSession{since: at}`.
    - `player_leave`: `CloseSession` by `(platformId, name, since=*e.Since)`. If not found, `InsertClosedSession` with `Since=*e.Since` (or `at` if `Since` is nil) and `Until=at`.
    - `world_saved`: no state change; the row in events is enough.
  - For every applied non-heartbeat event, `LastEventAt = max(LastEventAt, at)`. `StatusAt` is set whenever Status changes.
- **SweepStale:** for each server with open sessions, if `now - LastHeartbeat > HeartbeatTimeout` (or LastHeartbeat is zero), call `CloseAllOpen(until = lastSeen, reason "server_lost")`.
- **Status:**
  - `unknown` if there is no row, or LastHeartbeat and LastEventAt are both zero;
  - `offline` if `now - LastHeartbeat > 3m`;
  - otherwise the stored Status, falling back to `online` if it's empty.

- [ ] **Step 1: Write the failing tests** — `internal/live/apply_test.go`, using `store.Open("")` and a fixed `Now`. Required cases:
  1. join then leave closes the session with the agent's `seconds`;
  2. applying the same batch twice counts it as applied the first time and fully skipped the second;
  3. leave without join inserts a closed session;
  4. `server_boot` after a heartbeat at T closes open sessions with `until=T` and reason `server_lost`, even when the boot comes 30 s later, i.e. within 3 min;
  5. `server_stopped` closes with `until=stop.at`;
  6. invalid events (empty id, unknown type, far-future `at`) are skipped while the valid ones in the same batch still apply;
  7. SweepStale closes sessions only once the heartbeat is older than 3 min, using the last heartbeat as the end;
  8. the `Status` table covers unknown, offline, restarting, starting and online;
  9. `players_now` with 0 sets Players to 0.
- [ ] **Step 2: Run to verify it fails.** Then implement.
- [ ] **Step 3: Run** `go test ./internal/live/ -v`. Expected: PASS.
- [ ] **Step 4: Commit** (`feat(live): apply agent events into sessions and live state; sweep stale servers`, with the trailer).

---

### Task 4: Cookie codec and unlock rate limiter

**Files:** Create `internal/auth/cookie.go`, `internal/auth/auth_test.go`.

**Interfaces:**
```go
type Codec struct{ Key []byte; Now func() time.Time }
func (c Codec) Encode(ids []string) string            // base64url(json{"s":sorted unique ids,"exp":unix now+365d}) + "." + base64url(HMAC-SHA256(key, payload))
func (c Codec) Decode(v string) (ids []string, ok bool) // constant-time MAC check (hmac.Equal); rejects expired/malformed
const CookieName = "farsight"
type Limiter struct{ /* per-key token bucket */ }
func NewLimiter(perMinute, burst int, now func() time.Time) *Limiter
func (l *Limiter) Allow(key string) bool // consumes a token only when called; the caller calls it on each failed attempt, and checks Peek before trying
func (l *Limiter) Peek(key string) bool  // true if a token is available
```
- The limiter drops keys that have been idle for 1 h (a lazy sweep on access), so memory stays bounded.

- [ ] **Step 1: Write the failing tests.** Cover:
  - Encode/Decode round trip, with ids deduplicated and sorted;
  - a tampered payload fails, a tampered MAC fails, and a different key fails;
  - an expired value fails (via the `Now` override);
  - garbage fails;
  - the limiter allows `burst` failures, then blocks, then refills after `60/perMinute` seconds;
  - keys are independent;
  - an idle key is evicted.
- [ ] **Step 2: Run to verify it fails.** Implement, then run `go test ./internal/auth/ -v`. Expected: PASS.
- [ ] **Step 3: Commit** (`feat(auth): HMAC unlock cookie and per-IP limiter`, with the trailer).

---

### Task 5: Render speed-up with a per-worker corner-biome cache

The Plan 2 final review measured `TerrainHeight` at about 4 µs. Each call recomputes the four corner biomes of its 64 m zone, and a 256 px tile row crosses about 10 zones. Caching the corners per worker should roughly halve render CPU, which matters on a small pod.

**Files:** Create `internal/worldgen/sampler.go` and a test in `internal/worldgen/digest_test.go` or a new `sampler_test.go`. Modify `internal/tiles/tiles.go` so each worker uses a sampler when the source offers one.

**Interfaces:**
```go
// worldgen
type Sampler struct{ /* g *Generator; last zone (zx,zz) and its 4 corner biomes */ }
func (v TerrainView) NewSampler() *Sampler            // not safe for concurrent use; one per goroutine
func (s *Sampler) Biome(wx, wy float32) Biome           // = g.Biome
func (s *Sampler) Height(wx, wy float32) float32        // == g.TerrainHeight bit-for-bit, reusing corner biomes when the zone is unchanged
// tiles
type SamplerSource interface{ Source; NewSampler() Source } // optional; Render gives each worker its own
```
To make `(*worldgen.Sampler)` satisfy `tiles.Source`, `TerrainView.NewSampler` returns `*Sampler`. Tiles checks with an interface assertion for `interface{ NewSampler() *worldgen.Sampler }`. Tiles must not import worldgen more than it already does, since it already uses `worldgen.Biome`. Keep the adapter small.

- [ ] **Step 1: Write the failing tests.**
  - For FjordSeed gen 2, the sampler's `Height` is bit-identical (`math.Float32bits`) to `TerrainHeight` on the Task 5b digest grid, including zone-edge and corner points. Visit the points in both row order and random order so the cache is exercised across zone changes.
  - `TestTerrainDigest` still passes unchanged.
  - Add `BenchmarkSamplerRow`: 256 consecutive pixels at 2.56 m spacing. Compare it with `TerrainHeight` and report both ns/op.
- [ ] **Step 2: Implement.** Then make `tiles.Render` use a per-worker sampler when available. The tile tests must still pass, because the fake sources don't implement it.
- [ ] **Step 3: Run** `go test ./internal/worldgen/ ./internal/tiles/ -count=1` and the benchmark, then `go run ./cmd/farsight-render -seed FjordSeed -out /tmp/fs-tiles2` twice: once before the change (on `master`) and once after. Record the wall-clock times. The tile PNGs must be byte-identical before and after; check with `diff -r` on the two outputs.
- [ ] **Step 4: Commit** (`perf(worldgen,tiles): per-worker corner-biome sampler`, with the timings in the body and the trailer).

---

### Task 6: Tile set manager

**Files:** Create `internal/tileset/manager.go`, `internal/tileset/manager_test.go`.

**Interfaces:**
```go
type State string // "none","queued","rendering","complete","refused"
type Status struct{ State State; Done, Total int; Key string }
type RenderFunc func(ctx context.Context, seed, gen int32, dir string, progress func(done, total int)) error
type Manager struct{ /* root dir, render func, queue, current, statuses */ }
func NewManager(root string, render RenderFunc, log *slog.Logger) *Manager
func DefaultRender(workers int) RenderFunc // worldgen.NewChecked + tiles.Render(ctx, g.TerrainView(), tiles.Options{Dir, Workers}, progress)
func (m *Manager) Ensure(seed, gen int32) Status // idempotent; enqueues if not complete/queued/rendering; refused if gen<0||gen>worldgen.MaxGenVersion
func (m *Manager) Status(seed, gen int32) Status // complete iff tiles.Complete(dir)
func (m *Manager) Dir(seed, gen int32) string    // tiles.SetDir(root, seed, gen)
func (m *Manager) Key(seed, gen int32) string    // filepath.Base(Dir)
func (m *Manager) Run(ctx context.Context) error  // single worker: renders queued keys FIFO; after each completes, GC sibling dirs with the same "{seed}-{gen}-r" prefix but another render version; returns ctx.Err()
```
- A failed render logs Error and goes back to `none`. A later `Ensure` re-queues it, and the resume is cheap because tiles resume.
- The manager is safe for concurrent `Ensure`/`Status` calls from HTTP handlers.

- [ ] **Step 1: Write the failing tests**, with a fake `RenderFunc` that writes a `complete` marker via a small helper, reports progress, and can be blocked on a channel. Cover:
  - `Ensure` → queued → rendering (progress visible through `Status`) → complete;
  - two `Ensure` calls for the same key render once;
  - gen 3 is refused;
  - different keys render sequentially, never concurrently (assert with an atomic "in flight" counter);
  - a render error returns the key to `none`, and a later `Ensure` retries it;
  - GC removes `…-r0` and keeps `-r1`;
  - `Run` returns on cancel.
- [ ] **Step 2: Implement.** Run `go test ./internal/tileset/ -count=3 -v`. Expected: PASS.
- [ ] **Step 3: Commit** (`feat(tileset): background tile set manager, one render at a time`, with the trailer).

---

### Task 7: HTTP server with ingest, API, unlock and tiles

**Files:** Create `internal/server/{server,ingest,api,tiles}.go` and `internal/server/server_test.go`.

**Interfaces:**
```go
type Deps struct {
    Config  *config.Config
    Store   *store.Store
    Applier *live.Applier
    Tiles   *tileset.Manager
    Codec   auth.Codec
    Limiter *auth.Limiter
    IngestLimiter *auth.Limiter // nil => NewLimiter(10, 10, Now)
    Now     func() time.Time
    Log     *slog.Logger
    UI      http.Handler // Plan 4b static UI; nil => 404 for "/"
}
func New(d Deps) http.Handler
```
- **Routes:** the stdlib `http.ServeMux` patterns from the API contract, plus `POST /ingest/{server}/snapshot` and `POST /ingest/{server}/events`. `/` goes to UI.
- **Ingest:**
  - **Auth:** read `Authorization: Bearer`; a missing one gets 401. bcrypt is slow, so cache `sha256(token)` for 10 min after a successful compare; a cache hit skips everything below. On a miss, charge `IngestLimiter.Allow(ip)` first (none left → 429 `{"error":"too many attempts"}`), then compare; success refunds the charge. An unknown server gets a dummy compare and the same 401 `{"error":"unauthorized"}` as a bad token, so ingest can't probe server ids.
  - Every bcrypt compare in the server (unlock, the unlock dummy and ingest) waits on a process-wide 2-slot semaphore; if the request is cancelled while waiting, it returns 503 without comparing.
  - **Body:** require `Content-Encoding: gzip`. Cap the compressed size (`http.MaxBytesReader`) at 32 MiB and the decompressed size at 128 MiB for snapshots and 16 MiB for events. Over a limit returns 413; malformed JSON returns 400.
  - **Snapshot:**
    - Validate that `serverId` matches the path and that `saveId` and `savedAt` are non-zero, else 400.
    - `PutSnapshot` with the re-encoded JSON blob.
    - `Tiles.Ensure(world.seed, world.genVersion)`.
    - Respond 200 `{"stored":true|false}`.
  - **Events:** `Applier.Apply` → 200 `{"applied":n,"skipped":m}`.
- **Unlock:**
  - The client IP is RemoteAddr's host, or, when `TrustProxy` is set, the rightmost X-Forwarded-For entry (the address the trusted proxy saw).
  - If `Limiter.Peek` fails, return 429.
  - An unknown server, or a wrong passphrase (`bcrypt.CompareHashAndPassword`), calls `Limiter.Allow(ip)` and returns 401 with the same body in both cases, so unknown and wrong can't be told apart.
  - On success, merge the id into the cookie's existing set and set the cookie (`Path=/`, `MaxAge=365d`, HttpOnly, SameSite Lax, Secure per config). Return 204.
- **Gate:** `unlocked(r, id)` decodes the cookie. Server-scoped API and tile routes return 404 unless the server is unlocked.
- **Card:** assemble it per the API contract.
  - Status comes from `live.Status`.
  - `online`/`recent` come from the store: recent means sessions that ended in the last 72 h, capped at 50.
  - `activity` uses `RecentActivity(50)`.
  - `world` comes from the latest snapshot: decode it into `extract.Snapshot`.
    - `exploredPct` uses a package-level `var totalZones = countZonesWithin(10500)`, computed once, over 64 m zone centres.
    - `saveIntervalSec` is the median gap of `SaveTimes(10)` when there are at least 3.
  - `tiles` = `Tiles.Status(seed, gen)` plus key.
- **Snapshot API:** return the latest snapshot's `exploredZones`, `markers`, `bases`, `players`, `savedAt`, and `locations` filtered to explored zones. A location's zone is `(floor((x+32)/64), floor((z+32)/64))`.
- **Tiles:** match `/tiles/{id}/{key}/{z}/{x}/{y}.png`.
  - 404 unless the server is unlocked, the key equals its current snapshot's `Tiles.Key`, and that set is complete.
  - Validate z in 0..5 and x,y in range; serve the file with `http.ServeFile`, and add the immutable Cache-Control header.
- **Handlers never log request bodies or tokens.** Use `http.Server` timeouts (read header 10 s, read 2 min, write 2 min, idle 2 min). They are set in main (Task 8).

- [ ] **Step 1: Write the failing tests** — `internal/server/server_test.go`, using `httptest`, a real in-memory store, a real Applier, a tileset Manager with a fake render, and a config with 2 servers whose bcrypt hashes use MinCost. Required cases:
  1. ingest with no token or a wrong token → 401; unknown server → the same 401; not gzip → 400; oversized → 413;
  2. a valid snapshot is stored, a second identical POST returns `stored:false`, and the tile Ensure is triggered;
  3. an events batch returns applied/skipped counts, and replaying it gives `applied:0`;
  4. every server-scoped API route returns 404 without the cookie;
  5. unlock with a wrong passphrase → 401, and after `burst` failures → 429 even with the right passphrase; the right passphrase before that → 204 plus a cookie;
  6. with the cookie, `/api/servers` lists only the unlocked server, and unlocking a second one merges the cookie;
  7. the card has the right status, online players, activity, world bosses/day/exploredPct, and tiles state, after ingesting a snapshot and events built with `extract`/`logwatch` types;
  8. the snapshot API filters out a location in an unexplored zone and keeps one in an explored zone;
  9. tiles: 404 before complete, 200 with immutable caching after the fake render completes, 404 for a wrong key and for z=6;
  10. `/healthz` returns 200.
- [ ] **Step 2: Implement.** Run `go test ./internal/server/ -v`. Expected: PASS.
- [ ] **Step 3: Commit** (`feat(server): ingest, unlock, card/snapshot API and tile serving`, with the trailer).

---

### Task 8: `farsight serve`, background loops, end-to-end test, spec amendment

**Files:** Modify `cmd/farsight/main.go`. Create `internal/server/e2e_test.go`. Modify the spec.

**Behaviour of `farsight serve -config PATH`:**
- Load the config (exit 2 on error).
- `store.Open(filepath.Join(dataDir, "farsight.db"))`.
- `tileset.NewManager(filepath.Join(dataDir, "tiles"), tileset.DefaultRender(max(1, GOMAXPROCS-1)))`.
- On startup, call `Ensure` for every server's latest snapshot, so a missing tile set renders after a restart.
- Background loops:
  - `Applier.SweepStale` every 30 s;
  - `PruneSnapshots(now-14d)` and `PruneEvents(now-14d)` hourly;
  - `Tiles.Run`.
- An HTTP server with the timeouts above and `UI=nil`, since Plan 4b adds it.
- Shut down gracefully on SIGTERM: stop accepting, drain for up to 10 s, cancel the loops, close the store.
- Log with slog JSON: one line per ingest (server, kind, applied/skipped or stored, bytes). Never log tokens.

- [ ] **Step 1: Write the failing end-to-end test** — `internal/server/e2e_test.go`:
  - Start the full handler with `httptest.NewServer`.
  - Use the Plan 1 agent against a synthetic chunked world (`savetest.WriteChunkedWorld`, with a bed, a portal pair and a tame) and call `Tick` once. Assert the snapshot is stored.
  - Use a `logwatch.Watcher{Once:true}` over a temp log dir containing a crossplay join/leave plus one still-online player. Feed it into `agent.NewSink`, run it until it's delivered, then assert via the API with an unlock cookie:
    - the card shows status `online` (heartbeat present, ready line present);
    - one online player, one recent session with the right seconds;
    - `world.day` and the bosses;
    - the snapshot API returns the markers.
  - Then advance the Applier's `Now` by 4 min and run `SweepStale`. The card now shows `offline`, and the online player has moved to recent with reason `server_lost`.
- [ ] **Step 2: Implement `serve`.** Run `go test ./... && go build ./cmd/...`.
- [ ] **Step 3: Smoke test.** Hash a passphrase and a token with `farsight hash`, then write a temp config pointing `dataDir` at a temp dir. Run `farsight serve` in the background. Point `farsight-agent` (with env) at a copy of `testdata-golden/chunked` and `testdata-golden/logs/mulevikings` (with `FARSIGHT_LOG_TZ=Europe/Oslo`). After about 30 s, `curl` unlock and then the card, and check it by eye:
  - the players from today's log;
  - world day 278 or later;
  - tiles `rendering` and then, after about 25 s on this machine, `complete`;
  - fetch one z0 tile.
  Stop both processes. Put the card JSON excerpt in the report.
- [ ] **Step 4: Amend the spec** (`docs(spec): …`):
  - config is a JSON file (`farsight.json`), not YAML;
  - routing uses the stdlib mux, not chi;
  - replace the loose "Web API (JSON)" bullet with the API contract from this plan, verbatim;
  - tiles are served per server and key at `/tiles/{id}/{key}/…`;
  - unlocked-only access returns 404 rather than 403.
- [ ] **Step 5: Commit** (`feat(cmd): farsight serve with background loops; e2e test`, then the spec commit, each with the trailer).

---

## After this plan

- Plan 4b builds the SvelteKit UI (`web/`) against the API contract above, embeds it via `Deps.UI`, and implements the design's layouts and states.
- Plan 5 builds images for `farsight` and `farsight-agent` (amd64, `GOAMD64=v1`, with the digest test in the build) and adds the cloudcluster manifests.
