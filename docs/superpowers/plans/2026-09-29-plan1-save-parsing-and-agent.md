# Farsight Plan 1: Save Parsing, Snapshot Extraction and Agent Implementation Plan

> **Placeholders:** player names, platform IDs, join codes, IP addresses other than the public join address, and world seeds in this document are invented stand-ins. The real values were removed before the repository was published. Code snippets that assert specific seed names reflect the original private tests, which now check seed length and hash instead.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Read Valheim world saves (1.0 chunked and legacy `.db`), extract atlas snapshots, and ship a `farsight-agent` that pushes a snapshot to an HTTP endpoint after every autosave.

**Architecture:** One Go module with small internal packages. `zpkg` reads and writes Valheim's binary primitives. `names` holds the stable hash and the prefab-name table. `save` detects the format and streams ZDOs through a callback. `extract` turns the ZDOs into a JSON `Snapshot`. `agent` polls for new saves and POSTs snapshots. Two binaries: `farsight-agent` (the sidecar) and `farsight-snapshot` (a dev CLI).

**Tech Stack:** Go (latest stable, standard library only), `log/slog`, `net/http`, `compress/gzip`.

**Spec:** `docs/superpowers/specs/2026-09-29-farsight-atlas-mvp-design.md`. This plan covers the spec's `zpkg`, `save`, `names` and `extract` units and the save-watcher half of `farsight-agent`. Later plans: 2 world generation and tiles, 3 log watcher (the agent's log half), 4 central app, API and UI, 5 images and cluster deployment.

## Global Constraints

- Go module path: `github.com/jumpingmushroom/farsight`.
- Standard library only in this plan (no third-party modules).
- Supported world versions: legacy `.db` 31–39 (both live legacy worlds are 37), chunked 40–41. Anything else returns `save.ErrUnsupportedVersion`. Never guess.
- The agent never writes to, renames or deletes anything under the worlds directory.
- Stable hash is Valheim's `GetStableHashCode` over UTF-16 code units; test vectors: `"tag"` = 696029674, `"portal_wood"` = -661882940, `"Player_tombstone"` = -1558312669, `"FjordSeed"` = -1032944128.
- Ingest request: `POST {URL}/ingest/{serverID}/snapshot`, `Authorization: Bearer {token}`, `Content-Type: application/json`, `Content-Encoding: gzip`.
- The real-world golden data (`testdata-golden/`) is gitignored. Tests that need it call `t.Skip` when it is absent.
- Commit after every task, with messages in the form `feat(pkg): …`.

## File Structure

```
go.mod
.gitignore
hack/pull-golden.sh              # copies real saves from the cluster into testdata-golden/
hack/pull-manifests.sh           # copies SoftRef manifests from the cluster
cmd/gennames/main.go             # manifest -> internal/names/names.txt
cmd/farsight-snapshot/main.go    # dev CLI: world -> snapshot JSON on stdout
cmd/farsight-agent/main.go       # sidecar entrypoint
internal/zpkg/reader.go          # binary reader
internal/zpkg/writer.go          # binary writer (used by tests and savetest)
internal/zpkg/zpkg_test.go
internal/names/names.go          # StableHash, Lookup
internal/names/names.txt         # generated prefab/location names (checked in)
internal/names/names_test.go
internal/save/zdo.go             # ZDO type + decoder
internal/save/chunked.go         # 1.0 chunked format
internal/save/legacy.go          # legacy .db/.fwl
internal/save/world.go           # World, Meta, Read, LatestSave, errors
internal/save/zdo_test.go
internal/save/chunked_test.go
internal/save/legacy_test.go
internal/save/golden_test.go
internal/save/testdata/chunked/{small.chunk,portals.chunk,main.chunks,main.db2,main.fwl2}   # already committed
internal/save/savetest/savetest.go   # synthetic world writers for tests
internal/extract/snapshot.go     # Snapshot JSON types
internal/extract/tables.go       # location/boss tables
internal/extract/extract.go      # Extractor
internal/extract/bases.go        # base clustering
internal/extract/extract_test.go
internal/extract/bases_test.go
internal/extract/golden_test.go
internal/agent/agent.go
internal/agent/agent_test.go
```

---

### Task 1: Toolchain, module, and `zpkg` reader and writer

**Files:**
- Create: `go.mod`, `.gitignore`, `internal/zpkg/reader.go`, `internal/zpkg/writer.go`, `internal/zpkg/zpkg_test.go`

**Interfaces:**
- Produces: `zpkg.NewReader(b []byte) *Reader`, with methods `Err() error`, `Pos() int`, `Len() int`, `Bytes(n int) []byte`, `U8() uint8`, `Bool() bool`, `U16() uint16`, `I16() int16`, `U32() uint32`, `I32() int32`, `I64() int64`, `F32() float32`, `F64() float64`, `Vec3() [3]float32`, `Quat() [4]float32`, `Vec2s() [2]int16`, `Str() string`, `NumItems() int`, `ByteArray() []byte`. `zpkg.ErrShort`. `zpkg.Writer` (zero value usable) with `Bytes() []byte` and the matching writers `U8, Bool, U16, I16, U32, I32, I64, F32, F64, Vec3, Quat, Vec2s, Str, NumItems, ByteArray, Raw([]byte)`.
- Reader semantics: all little-endian. After the first short read, `Err()` stays `ErrShort` and every later read returns a zero value. `Str()` is .NET `BinaryReader.ReadString`: a 7-bit varint byte length, then UTF-8. `NumItems()` is one byte, or two bytes when the high bit is set: `((b0 & 0x7F) << 8) | b1`. `ByteArray()` is an `int32` length, then that many bytes.

- [ ] **Step 1: Install Go if missing and init the module**

```bash
cd /workspace/Farsight
command -v go || { V=$(curl -s 'https://go.dev/VERSION?m=text' | head -1); mkdir -p ~/.local; curl -sSL "https://go.dev/dl/${V}.linux-amd64.tar.gz" | tar -xz -C ~/.local; }
export PATH=$HOME/.local/go/bin:$PATH
go version
go mod init github.com/jumpingmushroom/farsight
printf 'testdata-golden/\n/farsight-agent\n/farsight-snapshot\n/gennames\n' > .gitignore
```
Expected: `go version go1.2x… linux/amd64`, and `go.mod` is created. Every later step assumes `export PATH=$HOME/.local/go/bin:$PATH`.

- [ ] **Step 2: Write the failing tests** — `internal/zpkg/zpkg_test.go`

```go
package zpkg

import (
	"bytes"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	var w Writer
	w.U8(7)
	w.Bool(true)
	w.U16(0xBEEF)
	w.I16(-2)
	w.U32(0xDEADBEEF)
	w.I32(-5)
	w.I64(-1 << 40)
	w.F32(1.5)
	w.F64(501766.85744524375)
	w.Vec3([3]float32{1, 2, 3})
	w.Quat([4]float32{0, 0, 0, 1})
	w.Vec2s([2]int16{-2, -5})
	w.Str("MuleVikings")
	w.Str(string(bytes.Repeat([]byte("x"), 300))) // two-byte varint length
	w.NumItems(5)
	w.NumItems(300) // two-byte form
	w.ByteArray([]byte{9, 8})

	r := NewReader(w.Bytes())
	if r.U8() != 7 || !r.Bool() || r.U16() != 0xBEEF || r.I16() != -2 || r.U32() != 0xDEADBEEF || r.I32() != -5 || r.I64() != -1<<40 {
		t.Fatal("integer round trip failed")
	}
	if r.F32() != 1.5 || r.F64() != 501766.85744524375 {
		t.Fatal("float round trip failed")
	}
	if r.Vec3() != [3]float32{1, 2, 3} || r.Quat() != [4]float32{0, 0, 0, 1} || r.Vec2s() != [2]int16{-2, -5} {
		t.Fatal("vector round trip failed")
	}
	if r.Str() != "MuleVikings" || len(r.Str()) != 300 {
		t.Fatal("string round trip failed")
	}
	if r.NumItems() != 5 || r.NumItems() != 300 {
		t.Fatal("numitems round trip failed")
	}
	if !bytes.Equal(r.ByteArray(), []byte{9, 8}) {
		t.Fatal("bytearray round trip failed")
	}
	if r.Err() != nil || r.Len() != 0 {
		t.Fatalf("err=%v len=%d", r.Err(), r.Len())
	}
}

func TestShortReadIsSticky(t *testing.T) {
	r := NewReader([]byte{1, 2, 3})
	if r.I32() != 0 || r.Err() != ErrShort {
		t.Fatalf("want ErrShort, got %v", r.Err())
	}
	if r.U8() != 0 || r.Err() != ErrShort {
		t.Fatal("error must be sticky")
	}
}

func TestKnownBytes(t *testing.T) {
	// "is 41" as int16 then int32 count 4, taken from a real chunk file header.
	r := NewReader([]byte{0x29, 0x00, 0x04, 0x00, 0x00, 0x00})
	if r.I16() != 41 || r.I32() != 4 {
		t.Fatal("header decode mismatch")
	}
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `go test ./internal/zpkg/`
Expected: FAIL with `undefined: Writer` / `undefined: NewReader`.

- [ ] **Step 4: Implement** — `internal/zpkg/reader.go`

```go
// Package zpkg reads and writes the little-endian primitives Valheim's
// ZPackage and .NET BinaryReader/BinaryWriter use in world saves.
package zpkg

import (
	"encoding/binary"
	"errors"
	"math"
)

var ErrShort = errors.New("zpkg: unexpected end of data")

var errBadLength = errors.New("zpkg: bad string length")

type Reader struct {
	b   []byte
	p   int
	err error
}

func NewReader(b []byte) *Reader { return &Reader{b: b} }

func (r *Reader) Err() error { return r.err }
func (r *Reader) Pos() int   { return r.p }
func (r *Reader) Len() int   { return len(r.b) - r.p }

func (r *Reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || r.p+n > len(r.b) {
		r.err = ErrShort
		r.p = len(r.b)
		return nil
	}
	v := r.b[r.p : r.p+n]
	r.p += n
	return v
}

func (r *Reader) Bytes(n int) []byte { return r.take(n) }

func (r *Reader) U8() uint8 {
	b := r.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func (r *Reader) Bool() bool { return r.U8() != 0 }

func (r *Reader) U16() uint16 {
	b := r.take(2)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint16(b)
}

func (r *Reader) I16() int16 { return int16(r.U16()) }

func (r *Reader) U32() uint32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}

func (r *Reader) I32() int32 { return int32(r.U32()) }

func (r *Reader) I64() int64 {
	b := r.take(8)
	if b == nil {
		return 0
	}
	return int64(binary.LittleEndian.Uint64(b))
}

func (r *Reader) F32() float32 { return math.Float32frombits(r.U32()) }
func (r *Reader) F64() float64 { return math.Float64frombits(uint64(r.I64())) }

func (r *Reader) Vec3() [3]float32   { return [3]float32{r.F32(), r.F32(), r.F32()} }
func (r *Reader) Quat() [4]float32   { return [4]float32{r.F32(), r.F32(), r.F32(), r.F32()} }
func (r *Reader) Vec2s() [2]int16    { return [2]int16{r.I16(), r.I16()} }
func (r *Reader) ByteArray() []byte  { return r.take(int(r.I32())) }

func (r *Reader) Str() string {
	n, shift := 0, 0
	for {
		b := r.U8()
		if r.err != nil {
			return ""
		}
		n |= int(b&0x7F) << shift
		if b&0x80 == 0 {
			break
		}
		shift += 7
		if shift > 28 {
			r.err = errBadLength
			return ""
		}
	}
	return string(r.take(n))
}

func (r *Reader) NumItems() int {
	n := int(r.U8())
	if n&0x80 != 0 {
		n = (n&0x7F)<<8 | int(r.U8())
	}
	return n
}
```

`internal/zpkg/writer.go`

```go
package zpkg

import (
	"encoding/binary"
	"math"
)

// Writer is the inverse of Reader. It exists for tests and test fixtures.
type Writer struct{ b []byte }

func (w *Writer) Bytes() []byte  { return w.b }
func (w *Writer) Raw(b []byte)   { w.b = append(w.b, b...) }
func (w *Writer) U8(v uint8)     { w.b = append(w.b, v) }
func (w *Writer) U16(v uint16)   { w.b = binary.LittleEndian.AppendUint16(w.b, v) }
func (w *Writer) I16(v int16)    { w.U16(uint16(v)) }
func (w *Writer) U32(v uint32)   { w.b = binary.LittleEndian.AppendUint32(w.b, v) }
func (w *Writer) I32(v int32)    { w.U32(uint32(v)) }
func (w *Writer) I64(v int64)    { w.b = binary.LittleEndian.AppendUint64(w.b, uint64(v)) }
func (w *Writer) F32(v float32)  { w.U32(math.Float32bits(v)) }
func (w *Writer) F64(v float64)  { w.I64(int64(math.Float64bits(v))) }

func (w *Writer) Bool(v bool) {
	if v {
		w.U8(1)
	} else {
		w.U8(0)
	}
}

func (w *Writer) Vec3(v [3]float32) { w.F32(v[0]); w.F32(v[1]); w.F32(v[2]) }
func (w *Writer) Quat(v [4]float32) { w.F32(v[0]); w.F32(v[1]); w.F32(v[2]); w.F32(v[3]) }
func (w *Writer) Vec2s(v [2]int16)  { w.I16(v[0]); w.I16(v[1]) }

func (w *Writer) Str(s string) {
	n := len(s)
	for n >= 0x80 {
		w.U8(byte(n) | 0x80)
		n >>= 7
	}
	w.U8(byte(n))
	w.b = append(w.b, s...)
}

func (w *Writer) NumItems(n int) {
	if n < 0x80 {
		w.U8(uint8(n))
		return
	}
	w.U8(uint8(n>>8) | 0x80)
	w.U8(uint8(n))
}

func (w *Writer) ByteArray(b []byte) { w.I32(int32(len(b))); w.Raw(b) }
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -l . ; go vet ./... && go test ./internal/zpkg/`
Expected: no gofmt output, `ok  github.com/jumpingmushroom/farsight/internal/zpkg`.

- [ ] **Step 6: Commit**

```bash
git add go.mod .gitignore internal/zpkg
git commit -m "feat(zpkg): binary reader and writer for Valheim save primitives"
```

---

### Task 2: `names`: stable hash and the prefab name table

**Files:**
- Create: `internal/names/names.go`, `internal/names/names_test.go`, `internal/names/names.txt` (generated), `cmd/gennames/main.go`, `hack/pull-manifests.sh`

**Interfaces:**
- Produces: `names.StableHash(s string) int32`, `names.Lookup(h int32) (string, bool)`, `names.Name(h int32) string` (returns `""` when unknown), `names.Count() int`.
- The table (`names.txt`, one name per line, sorted) is every `Assets/…/<Name>.prefab` basename in `valheim_server_Data/StreamingAssets/SoftRef/manifest` and `manifest_extended`. The spike showed this alone resolves all 469 prefabs and 176 location types in the `mulevikings` world.

- [ ] **Step 1: Write the failing tests** — `internal/names/names_test.go`

```go
package names

import "testing"

func TestStableHash(t *testing.T) {
	cases := map[string]int32{
		"tag":              696029674,
		"portal_wood":      -661882940,
		"Player_tombstone": -1558312669,
		"FjordSeed":        -1032944128,
		"creator":          881008290,
		"tagauthor":        -1565613777,
		"":                 371857150, // 5381 + 5381*1566083941 wrapped to int32
	}
	for s, want := range cases {
		if got := StableHash(s); got != want {
			t.Errorf("StableHash(%q) = %d, want %d", s, got, want)
		}
	}
}

func TestTableResolvesKnownPrefabs(t *testing.T) {
	if Count() < 6000 {
		t.Fatalf("name table has %d entries, want > 6000 (regenerate names.txt)", Count())
	}
	for _, s := range []string{"portal_wood", "Player_tombstone", "bed", "Eikthyrnir", "DN_Bossroom", "Crypt2", "Wolf", "_ZoneCtrl"} {
		if got := Name(StableHash(s)); got != s {
			t.Errorf("Name(StableHash(%q)) = %q", s, got)
		}
	}
	if _, ok := Lookup(12345); ok {
		t.Error("Lookup of unknown hash must report false")
	}
}
```

All vectors, including the empty string, were computed with the spike's reference implementation, which resolved 100% of a live world's prefabs.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/names/`
Expected: FAIL with `undefined: StableHash`.

- [ ] **Step 3: Implement the hash and table** — `internal/names/names.go`

```go
// Package names maps Valheim's stable string hashes back to prefab and
// location names.
package names

import (
	_ "embed"
	"strings"
	"unicode/utf16"
)

// StableHash is Valheim's String.GetStableHashCode: two interleaved djb2-xor
// hashes over UTF-16 code units, stopping at a NUL, combined with a
// multiplier. int32 arithmetic wraps exactly like C#'s unchecked int.
func StableHash(s string) int32 {
	u := utf16.Encode([]rune(s))
	var n1, n2 int32 = 5381, 5381
	for i := 0; i < len(u) && u[i] != 0; i += 2 {
		n1 = ((n1 << 5) + n1) ^ int32(u[i])
		if i == len(u)-1 || u[i+1] == 0 {
			break
		}
		n2 = ((n2 << 5) + n2) ^ int32(u[i+1])
	}
	return n1 + n2*1566083941
}

//go:embed names.txt
var raw string

var table = func() map[int32]string {
	m := make(map[int32]string, 8000)
	for _, line := range strings.Split(raw, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			m[StableHash(line)] = line
		}
	}
	return m
}()

func Lookup(h int32) (string, bool) { s, ok := table[h]; return s, ok }
func Name(h int32) string           { return table[h] }
func Count() int                    { return len(table) }
```

- [ ] **Step 4: Write the generator and the manifest pull script**

`hack/pull-manifests.sh`

```bash
#!/usr/bin/env bash
# Copies the SoftRef manifests (prefab name source) from a running Valheim pod.
set -euo pipefail
: "${KUBECONFIG:=$HOME/.kube/cloudcluster.yaml}"; export KUBECONFIG
OUT=${1:-/tmp/farsight-manifests}
mkdir -p "$OUT"
SRC=/opt/valheim/server/valheim_server_Data/StreamingAssets/SoftRef
for f in manifest manifest_extended; do
  kubectl -n gameservers exec deploy/mulevikings-valheim -c valheim -- cat "$SRC/$f" > "$OUT/$f"
done
ls -la "$OUT"
```

`cmd/gennames/main.go`

```go
// Command gennames builds internal/names/names.txt from Valheim's SoftRef
// manifests: every Assets/.../<Name>.prefab basename, sorted and unique.
//
//	go run ./cmd/gennames -o internal/names/names.txt manifest manifest_extended
package main

import (
	"flag"
	"log"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
)

var prefabPath = regexp.MustCompile(`Assets/[A-Za-z0-9_/ ().-]+\.prefab`)

func main() {
	out := flag.String("o", "internal/names/names.txt", "output file")
	flag.Parse()
	seen := map[string]bool{}
	for _, f := range flag.Args() {
		b, err := os.ReadFile(f)
		if err != nil {
			log.Fatal(err)
		}
		for _, m := range prefabPath.FindAll(b, -1) {
			seen[strings.TrimSuffix(path.Base(string(m)), ".prefab")] = true
		}
	}
	list := make([]string, 0, len(seen))
	for n := range seen {
		list = append(list, n)
	}
	sort.Strings(list)
	if len(list) == 0 {
		log.Fatal("no prefab paths found; wrong input files?")
	}
	if err := os.WriteFile(*out, []byte(strings.Join(list, "\n")+"\n"), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %d names to %s", len(list), *out)
}
```

- [ ] **Step 5: Generate the table**

```bash
chmod +x hack/pull-manifests.sh
hack/pull-manifests.sh /tmp/farsight-manifests
go run ./cmd/gennames -o internal/names/names.txt /tmp/farsight-manifests/manifest /tmp/farsight-manifests/manifest_extended
```
Expected: `wrote 6507 names to internal/names/names.txt` (±, after game updates).

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/names/`
Expected: `ok`.

- [ ] **Step 7: Commit**

```bash
git add internal/names cmd/gennames hack/pull-manifests.sh
git commit -m "feat(names): stable hash and prefab name table from SoftRef manifests"
```

---

### Task 3: `save`: the ZDO decoder and synthetic writers

**Files:**
- Create: `internal/save/zdo.go`, `internal/save/world.go` (types and errors only in this task), `internal/save/savetest/savetest.go`, `internal/save/zdo_test.go`
- Test fixtures (already committed): `internal/save/testdata/chunked/small.chunk`, `portals.chunk`

**Interfaces:**
- Consumes: `zpkg.Reader`, `zpkg.Writer`, `names.StableHash`.
- Produces:
  - `save.ZDO{Pos [3]float32; Prefab int32; Floats map[int32]float32; Vec3s map[int32][3]float32; Ints map[int32]int32; Longs map[int32]int64; Strings map[int32]string}`. Maps are nil when that section is absent. Quaternions, byte arrays and connections are read and discarded.
  - `save.DecodeZDO(r *zpkg.Reader, version int32, z *ZDO) error` overwrites `*z` completely.
  - Version constants `save.VersionNewSaveFormat = 31`, `VersionNumItems = 33`, `VersionChunkedSave = 40`, `VersionDeepNorth = 41`.
  - `save.ErrUnsupportedVersion`, `save.ErrSaveChanged`, `save.ErrNoSave`, `save.ErrSaveInProgress`.
  - `savetest.ZDO{Pos [3]float32; Prefab string; Floats map[string]float32; Ints map[string]int32; Longs map[string]int64; Strings map[string]string}`
  - `savetest.EncodeZDO(w *zpkg.Writer, version int32, z ZDO)`
- ZDO record layout, from `ZDO.Load` in `assembly_valheim.dll`:
  1. `u16 flags`. Bit masks: `0x01` connections, `0x02` floats, `0x04` vec3, `0x08` quats, `0x10` ints, `0x20` longs, `0x40` strings, `0x80` byte arrays, `0x100` persistent, `0x200` distant, `0xC00` type, `0x1000` rotation, `0x2000` small position.
  2. Only when version < 40: a `Vec2s` sector, which is discarded.
  3. Position: `Vec2s` giving `(x, 0, z)` when the small-position bit is set, otherwise `Vec3`.
  4. `i32 prefab`.
  5. Rotation, only when its bit is set. For version ≥ 40: one `u16`, plus a second `u16` when the first lacks `0x8000`. For version < 40: a `Vec3`.
  6. Only when `flags & 0xFF` is non-zero: connections (`u8`, `i32`) if flagged, then for each flagged type in the order floats, vec3, quats, ints, longs, strings, byte arrays: a count, then `count × (i32 key, value)`. The count is one byte for version < 33, otherwise `NumItems`.

- [ ] **Step 1: Write the failing tests** — `internal/save/zdo_test.go`

```go
package save

import (
	"os"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/save/savetest"
	"github.com/jumpingmushroom/farsight/internal/zpkg"
)

func readChunkFixture(t *testing.T, name string) []ZDO {
	t.Helper()
	b, err := os.ReadFile("testdata/chunked/" + name)
	if err != nil {
		t.Fatal(err)
	}
	r := zpkg.NewReader(b)
	ver := int32(r.I16())
	n := int(r.I32())
	out := make([]ZDO, n)
	for i := range out {
		if err := DecodeZDO(r, ver, &out[i]); err != nil {
			t.Fatalf("zdo %d: %v", i, err)
		}
	}
	if r.Len() != 0 {
		t.Fatalf("%d trailing bytes", r.Len())
	}
	return out
}

func TestDecodeSmallChunk(t *testing.T) {
	z := readChunkFixture(t, "small.chunk")
	if len(z) != 4 {
		t.Fatalf("got %d zdos, want 4", len(z))
	}
	if names.Name(z[0].Prefab) != "_ZoneCtrl" || z[0].Pos != [3]float32{1024, 0, 0} || z[1].Pos != [3]float32{1024, 0, 64} {
		t.Fatalf("unexpected first zdos: %+v %+v", z[0], z[1])
	}
}

func TestDecodePortalChunk(t *testing.T) {
	z := readChunkFixture(t, "portals.chunk")
	if len(z) != 16 {
		t.Fatalf("got %d zdos, want 16", len(z))
	}
	tags := map[string]int{}
	for _, p := range z {
		if names.Name(p.Prefab) != "portal_wood" {
			t.Errorf("prefab %q, want portal_wood", names.Name(p.Prefab))
		}
		tags[p.Strings[names.StableHash("tag")]]++
	}
	for _, tag := range []string{"Harbor", "Meadow", "Brynhild", "Fjell", "Thorgerdr", "Vale", "Summit", "Stonewatch"} {
		if tags[tag] != 2 {
			t.Errorf("tag %q count %d, want 2", tag, tags[tag])
		}
	}
	first := z[0]
	if first.Pos != [3]float32{-1525.9298095703125, 39.51213836669922, 982.9947509765625} {
		t.Errorf("pos %v", first.Pos)
	}
	if first.Longs[names.StableHash("creator")] != 1000000002 {
		t.Errorf("creator %v", first.Longs)
	}
}

func TestEncodeDecodeRoundTripBothVersions(t *testing.T) {
	in := savetest.ZDO{
		Pos: [3]float32{10.5, 33, -20.25}, Prefab: "bed",
		Floats:  map[string]float32{"health": 80},
		Ints:    map[string]int32{"tamed": 1},
		Longs:   map[string]int64{"owner": 1000000099},
		Strings: map[string]string{"ownerName": "Sigrun"},
	}
	for _, ver := range []int32{37, 41} {
		var w zpkg.Writer
		savetest.EncodeZDO(&w, ver, in)
		var out ZDO
		r := zpkg.NewReader(w.Bytes())
		if err := DecodeZDO(r, ver, &out); err != nil || r.Len() != 0 {
			t.Fatalf("v%d: err=%v trailing=%d", ver, err, r.Len())
		}
		if out.Pos != in.Pos || out.Prefab != names.StableHash("bed") ||
			out.Floats[names.StableHash("health")] != 80 ||
			out.Ints[names.StableHash("tamed")] != 1 ||
			out.Longs[names.StableHash("owner")] != 1000000099 ||
			out.Strings[names.StableHash("ownerName")] != "Sigrun" {
			t.Fatalf("v%d: round trip mismatch: %+v", ver, out)
		}
	}
}

func TestDecodeTruncated(t *testing.T) {
	var out ZDO
	if err := DecodeZDO(zpkg.NewReader([]byte{0x02, 0x00}), 41, &out); err == nil {
		t.Fatal("want error on truncated zdo")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/save/`
Expected: FAIL with `undefined: ZDO` / `no required module provides package …/savetest`.

- [ ] **Step 3: Implement** — `internal/save/world.go` (types for now; `Read` and `LatestSave` come in Task 5)

```go
// Package save reads Valheim world saves: the 1.0 chunked directory format
// (world version 40-41) and the legacy single-file .db (versions 31-39).
package save

import "errors"

const (
	VersionNewSaveFormat = 31
	VersionGlobalKeys    = 32
	VersionNumItems      = 33
	VersionChunkedSave   = 40
	VersionDeepNorth     = 41
)

var (
	ErrUnsupportedVersion = errors.New("save: unsupported world version")
	ErrSaveChanged        = errors.New("save: save changed while reading; retry")
	ErrNoSave             = errors.New("save: no world save found")
	ErrSaveInProgress     = errors.New("save: a save is being written")
)
```

`internal/save/zdo.go`

```go
package save

import "github.com/jumpingmushroom/farsight/internal/zpkg"

type ZDO struct {
	Pos     [3]float32
	Prefab  int32
	Floats  map[int32]float32
	Vec3s   map[int32][3]float32
	Ints    map[int32]int32
	Longs   map[int32]int64
	Strings map[int32]string
}

const (
	flagConnections = 0x01
	flagFloats      = 0x02
	flagVec3        = 0x04
	flagQuats       = 0x08
	flagInts        = 0x10
	flagLongs       = 0x20
	flagStrings     = 0x40
	flagByteArrays  = 0x80
	flagAnyData     = 0xFF
	flagRotation    = 0x1000
	flagSmallPos    = 0x2000
)

// DecodeZDO reads one ZDO record written by the given world version.
func DecodeZDO(r *zpkg.Reader, version int32, z *ZDO) error {
	*z = ZDO{}
	chunked := version >= VersionChunkedSave
	flags := r.U16()
	if !chunked {
		r.Vec2s() // sector, recomputed by the game from the position
	}
	if flags&flagSmallPos != 0 {
		s := r.Vec2s()
		z.Pos = [3]float32{float32(s[0]), 0, float32(s[1])}
	} else {
		z.Pos = r.Vec3()
	}
	z.Prefab = r.I32()
	if flags&flagRotation != 0 {
		if chunked {
			if r.U16()&0x8000 == 0 {
				r.U16()
			}
		} else {
			r.Vec3()
		}
	}
	if flags&flagAnyData == 0 {
		return r.Err()
	}
	count := func() int {
		if version < VersionNumItems {
			return int(r.U8())
		}
		return r.NumItems()
	}
	if flags&flagConnections != 0 {
		r.U8()
		r.I32()
	}
	if flags&flagFloats != 0 {
		n := count()
		z.Floats = make(map[int32]float32, n)
		for i := 0; i < n; i++ {
			k := r.I32()
			z.Floats[k] = r.F32()
		}
	}
	if flags&flagVec3 != 0 {
		n := count()
		z.Vec3s = make(map[int32][3]float32, n)
		for i := 0; i < n; i++ {
			k := r.I32()
			z.Vec3s[k] = r.Vec3()
		}
	}
	if flags&flagQuats != 0 {
		n := count()
		for i := 0; i < n; i++ {
			r.I32()
			r.Quat()
		}
	}
	if flags&flagInts != 0 {
		n := count()
		z.Ints = make(map[int32]int32, n)
		for i := 0; i < n; i++ {
			k := r.I32()
			z.Ints[k] = r.I32()
		}
	}
	if flags&flagLongs != 0 {
		n := count()
		z.Longs = make(map[int32]int64, n)
		for i := 0; i < n; i++ {
			k := r.I32()
			z.Longs[k] = r.I64()
		}
	}
	if flags&flagStrings != 0 {
		n := count()
		z.Strings = make(map[int32]string, n)
		for i := 0; i < n; i++ {
			k := r.I32()
			z.Strings[k] = r.Str()
		}
	}
	if flags&flagByteArrays != 0 {
		n := count()
		for i := 0; i < n; i++ {
			r.I32()
			r.ByteArray()
		}
	}
	return r.Err()
}
```

`internal/save/savetest/savetest.go` (this task adds only `ZDO` and `EncodeZDO`; Tasks 4 and 5 add the world writers to this same file)

```go
// Package savetest writes synthetic Valheim saves for tests.
package savetest

import (
	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/zpkg"
)

// ZDO is a test-friendly ZDO: prefab and keys are names, hashed on encode.
type ZDO struct {
	Pos     [3]float32
	Prefab  string
	Floats  map[string]float32
	Ints    map[string]int32
	Longs   map[string]int64
	Strings map[string]string
}

// EncodeZDO writes z in the record layout of the given world version
// (legacy 31-39 or chunked 40-41). Positions are always full Vec3 and
// rotation is never written.
func EncodeZDO(w *zpkg.Writer, version int32, z ZDO) {
	var flags uint16
	if len(z.Floats) > 0 {
		flags |= 0x02
	}
	if len(z.Ints) > 0 {
		flags |= 0x10
	}
	if len(z.Longs) > 0 {
		flags |= 0x20
	}
	if len(z.Strings) > 0 {
		flags |= 0x40
	}
	w.U16(flags)
	if version < 40 {
		w.Vec2s([2]int16{0, 0})
	}
	w.Vec3(z.Pos)
	w.I32(names.StableHash(z.Prefab))
	count := func(n int) {
		if version < 33 {
			w.U8(uint8(n))
		} else {
			w.NumItems(n)
		}
	}
	if len(z.Floats) > 0 {
		count(len(z.Floats))
		for k, v := range z.Floats {
			w.I32(names.StableHash(k))
			w.F32(v)
		}
	}
	if len(z.Ints) > 0 {
		count(len(z.Ints))
		for k, v := range z.Ints {
			w.I32(names.StableHash(k))
			w.I32(v)
		}
	}
	if len(z.Longs) > 0 {
		count(len(z.Longs))
		for k, v := range z.Longs {
			w.I32(names.StableHash(k))
			w.I64(v)
		}
	}
	if len(z.Strings) > 0 {
		count(len(z.Strings))
		for k, v := range z.Strings {
			w.I32(names.StableHash(k))
			w.Str(v)
		}
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -l . ; go vet ./... && go test ./internal/save/...`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/save
git commit -m "feat(save): ZDO decoder for legacy and chunked versions, test encoder"
```

---

### Task 4: `save`: the chunked (1.0) world format

**Files:**
- Create: `internal/save/chunked.go`, `internal/save/chunked_test.go`
- Modify: `internal/save/world.go` (add `Format`, `Meta`, `Location`, `World`), `internal/save/savetest/savetest.go` (add `WriteChunkedWorld`)
- Fixtures (committed): `internal/save/testdata/chunked/main.chunks`, `main.db2`, `main.fwl2`

**Interfaces:**
- Consumes: `DecodeZDO`, `zpkg`, `names.StableHash`.
- Produces (in `world.go`):
  ```go
  type Format string
  const (FormatChunked Format = "chunked"; FormatLegacy Format = "legacy")
  type Meta struct { Name, SeedName string; Seed int32; UID int64; GenVersion int32; StartingKeys []string }
  type Location struct { Hash int32; Pos [3]float32; Placed bool }
  type World struct {
      Format Format; Version int32; SaveID string; Meta Meta; NetTime float64; ZDOCount int
      Zones [][2]int16; GlobalKeys []string; Locations []Location
  }
  ```
- Produces (in `chunked.go`), unexported but used by `Read` in Task 6:
  - `readFWL(b []byte) (int32, Meta, error)` parses the `.fwl` and `.fwl2` metadata; the layout is the same, gated by version.
  - `type chunkRef struct { Chunk uint16; Size uint8; Version uint32; ZDOs int32 }` and `func (c chunkRef) fileName() string`.
  - `readChunkIndex(b []byte) (total int32, refs []chunkRef, err error)`
  - `readChunkFile(b []byte, fn func(*ZDO)) (int, error)`
  - `readDB2(b []byte) (netTime float64, zs zoneState, err error)`
  - `type zoneState struct { Zones [][2]int16; GlobalKeys []string; Locations []Location }`
  - `latestChunkedSave(dir string) (n uint64, ok bool, err error)`, where `dir` is `<worldsDir>/<worldName>`.
  - `readChunked(dir string, fn func(*ZDO)) (*World, error)`
- `savetest.WriteChunkedWorld(t testing.TB, worldsDir, worldName string, saveNumber int, seedName string, zdos []ZDO, zones [][2]int16, keys []string, locs []Location)`, with `savetest.Location{Name string; Pos [3]float32}`.
- Formats, from `assembly_valheim.dll`:
  - `.fwl`/`.fwl2`: `i32 len`, then a package of `len` bytes: `i32 version`, `Str name`, `Str seedName`, `i32 seed`, `i64 uid`; `i32 genVersion` if version ≥ 26; `Bool needsDB` if ≥ 30; `i32 n` plus `n × Str` starting keys if ≥ 32. Anything after that, such as the version 41 player roster, is ignored.
  - `_main.N.chunks`: `i16 version`, `i32 totalZDOs`, `i32 n`, then `n × {u16 chunk, u8 size, u32 version, i32 zdoCount}`.
  - Chunk file name: `fmt.Sprintf("%02x_%02x__%d_%d.chunk", chunk>>8, chunk&0xFF, size, version)`.
  - Chunk file: `i16 version`, `i32 count`, then `count` ZDOs decoded with **that file's** version.
  - `_main.N.db2`: `i32 version`, `f64 netTime`, `i32 len`, then `len` bytes of **gzip**. The decompressed package holds: `i32 nZones`, `nZones × Vec2s`, `i32 locationVersion`, `i32 nKeys`, `nKeys × Str`, `Bool locationsGenerated`, `i32 nLocs`, and `nLocs × {i32 hash, Vec3, Bool placed}`.
  - `_main.N.ok` is written last. The newest complete save is the highest `N` for which `.ok` exists.

- [ ] **Step 1: Write the failing tests** — `internal/save/chunked_test.go`

```go
package save

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/save/savetest"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata/chunked", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReadFWL2Fixture(t *testing.T) {
	ver, m, err := readFWL(fixture(t, "main.fwl2"))
	if err != nil {
		t.Fatal(err)
	}
	if ver != 41 || m.Name != "MuleVikings" || m.SeedName != "FjordSeed" || m.Seed != -1032944128 || m.UID != 1234567890 || m.GenVersion != 2 {
		t.Fatalf("meta = %d %+v", ver, m)
	}
	if len(m.StartingKeys) != 14 || m.StartingKeys[0] != "teleportall" {
		t.Fatalf("starting keys = %q", m.StartingKeys)
	}
}

func TestReadChunkIndexFixture(t *testing.T) {
	total, refs, err := readChunkIndex(fixture(t, "main.chunks"))
	if err != nil {
		t.Fatal(err)
	}
	if total != 476157 || len(refs) != 37 {
		t.Fatalf("total=%d refs=%d", total, len(refs))
	}
	var sum int32
	for _, r := range refs {
		sum += r.ZDOs
	}
	if sum != total {
		t.Fatalf("sum of chunk counts %d != total %d", sum, total)
	}
	if got := (chunkRef{Chunk: 0x1e20, Size: 1, Version: 317}).fileName(); got != "1e_20__1_317.chunk" {
		t.Fatalf("fileName = %q", got)
	}
}

func TestReadDB2Fixture(t *testing.T) {
	nt, zs, err := readDB2(fixture(t, "main.db2"))
	if err != nil {
		t.Fatal(err)
	}
	if nt != 501766.85744524375 || len(zs.Zones) != 5233 || len(zs.Locations) != 12298 {
		t.Fatalf("netTime=%v zones=%d locs=%d", nt, len(zs.Zones), len(zs.Locations))
	}
	if zs.Zones[0] != [2]int16{-2, -5} {
		t.Fatalf("first zone %v", zs.Zones[0])
	}
	want := []string{"activebosses 0", "defeated_eikthyr", "killedtroll", "defeated_gdking", "defeated_writhan"}
	if len(zs.GlobalKeys) != len(want) {
		t.Fatalf("keys %q", zs.GlobalKeys)
	}
	for i := range want {
		if zs.GlobalKeys[i] != want[i] {
			t.Fatalf("keys %q", zs.GlobalKeys)
		}
	}
	unknown := 0
	for _, l := range zs.Locations {
		if _, ok := names.Lookup(l.Hash); !ok {
			unknown++
		}
	}
	if unknown != 0 {
		t.Fatalf("%d locations with unknown names", unknown)
	}
}

func TestReadChunkedSynthetic(t *testing.T) {
	dir := t.TempDir()
	zdos := []savetest.ZDO{
		{Pos: [3]float32{1, 2, 3}, Prefab: "portal_wood", Strings: map[string]string{"tag": "home"}},
		{Pos: [3]float32{4, 5, 6}, Prefab: "portal_wood", Strings: map[string]string{"tag": "home"}},
	}
	savetest.WriteChunkedWorld(t, dir, "Test", 7, "abc", zdos, [][2]int16{{0, 0}, {1, 0}},
		[]string{"defeated_eikthyr"}, []savetest.Location{{Name: "Eikthyrnir", Pos: [3]float32{100, 30, 200}}})
	n, ok, err := latestChunkedSave(filepath.Join(dir, "Test"))
	if err != nil || !ok || n != 7 {
		t.Fatalf("latest = %d %v %v", n, ok, err)
	}
	var got []ZDO
	w, err := readChunked(filepath.Join(dir, "Test"), func(z *ZDO) { got = append(got, *z) })
	if err != nil {
		t.Fatal(err)
	}
	if w.Format != FormatChunked || w.SaveID != "chunked:7" || w.ZDOCount != 2 || len(got) != 2 ||
		w.Meta.SeedName != "abc" || len(w.Zones) != 2 || w.GlobalKeys[0] != "defeated_eikthyr" ||
		names.Name(w.Locations[0].Hash) != "Eikthyrnir" {
		t.Fatalf("world = %+v", w)
	}
}

func TestReadChunkedMissingChunkIsSaveChanged(t *testing.T) {
	dir := t.TempDir()
	savetest.WriteChunkedWorld(t, dir, "Test", 1, "abc",
		[]savetest.ZDO{{Prefab: "bed"}}, nil, nil, nil)
	matches, _ := filepath.Glob(filepath.Join(dir, "Test", "*.chunk"))
	for _, m := range matches {
		os.Remove(m)
	}
	if _, err := readChunked(filepath.Join(dir, "Test"), func(*ZDO) {}); err != ErrSaveChanged {
		t.Fatalf("err = %v, want ErrSaveChanged", err)
	}
}
```

The fixture's 14 starting keys (`teleportall`, then repeated `preset …` strings) were verified with the spike parser.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/save/`
Expected: FAIL with `undefined: readFWL` (and friends).

- [ ] **Step 3: Implement the types** — append to `internal/save/world.go`

```go
type Format string

const (
	FormatChunked Format = "chunked"
	FormatLegacy  Format = "legacy"
)

type Meta struct {
	Name         string
	SeedName     string
	Seed         int32
	UID          int64
	GenVersion   int32
	StartingKeys []string
}

type Location struct {
	Hash   int32
	Pos    [3]float32
	Placed bool
}

type World struct {
	Format     Format
	Version    int32
	SaveID     string
	Meta       Meta
	NetTime    float64
	ZDOCount   int
	Zones      [][2]int16
	GlobalKeys []string
	Locations  []Location
}
```

- [ ] **Step 4: Implement** — `internal/save/chunked.go`

```go
package save

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/jumpingmushroom/farsight/internal/zpkg"
)

func readFWL(b []byte) (int32, Meta, error) {
	outer := zpkg.NewReader(b)
	r := zpkg.NewReader(outer.Bytes(int(outer.I32())))
	if outer.Err() != nil {
		return 0, Meta{}, outer.Err()
	}
	var m Meta
	ver := r.I32()
	m.Name = r.Str()
	m.SeedName = r.Str()
	m.Seed = r.I32()
	m.UID = r.I64()
	if ver >= 26 {
		m.GenVersion = r.I32()
	}
	if ver >= 30 {
		r.Bool() // needsDB
	}
	if ver >= VersionGlobalKeys {
		n := int(r.I32())
		for i := 0; i < n && r.Err() == nil; i++ {
			m.StartingKeys = append(m.StartingKeys, r.Str())
		}
	}
	return ver, m, r.Err()
}

type chunkRef struct {
	Chunk   uint16
	Size    uint8
	Version uint32
	ZDOs    int32
}

func (c chunkRef) fileName() string {
	return fmt.Sprintf("%02x_%02x__%d_%d.chunk", c.Chunk>>8, c.Chunk&0xFF, c.Size, c.Version)
}

func readChunkIndex(b []byte) (int32, []chunkRef, error) {
	r := zpkg.NewReader(b)
	r.I16()
	total := r.I32()
	n := int(r.I32())
	refs := make([]chunkRef, 0, n)
	for i := 0; i < n && r.Err() == nil; i++ {
		refs = append(refs, chunkRef{Chunk: r.U16(), Size: r.U8(), Version: r.U32(), ZDOs: r.I32()})
	}
	return total, refs, r.Err()
}

func readChunkFile(b []byte, fn func(*ZDO)) (int, error) {
	r := zpkg.NewReader(b)
	ver := int32(r.I16())
	if ver < VersionChunkedSave || ver > VersionDeepNorth {
		return 0, fmt.Errorf("%w: chunk version %d", ErrUnsupportedVersion, ver)
	}
	n := int(r.I32())
	var z ZDO
	for i := 0; i < n; i++ {
		if err := DecodeZDO(r, ver, &z); err != nil {
			return i, fmt.Errorf("zdo %d: %w", i, err)
		}
		fn(&z)
	}
	if r.Len() != 0 {
		return n, fmt.Errorf("save: %d trailing bytes in chunk", r.Len())
	}
	return n, nil
}

type zoneState struct {
	Zones      [][2]int16
	GlobalKeys []string
	Locations  []Location
}

func readDB2(b []byte) (float64, zoneState, error) {
	outer := zpkg.NewReader(b)
	outer.I32() // version
	netTime := outer.F64()
	gz := outer.Bytes(int(outer.I32()))
	if outer.Err() != nil {
		return 0, zoneState{}, outer.Err()
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return 0, zoneState{}, err
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return 0, zoneState{}, err
	}
	r := zpkg.NewReader(raw)
	var zs zoneState
	nz := int(r.I32())
	zs.Zones = make([][2]int16, 0, nz)
	for i := 0; i < nz && r.Err() == nil; i++ {
		zs.Zones = append(zs.Zones, r.Vec2s())
	}
	r.I32() // location version
	nk := int(r.I32())
	for i := 0; i < nk && r.Err() == nil; i++ {
		zs.GlobalKeys = append(zs.GlobalKeys, r.Str())
	}
	r.Bool() // locations generated
	nl := int(r.I32())
	zs.Locations = make([]Location, 0, nl)
	for i := 0; i < nl && r.Err() == nil; i++ {
		zs.Locations = append(zs.Locations, Location{Hash: r.I32(), Pos: r.Vec3(), Placed: r.Bool()})
	}
	return netTime, zs, r.Err()
}

var okFile = regexp.MustCompile(`^_main\.(\d+)\.ok$`)

func latestChunkedSave(dir string) (uint64, bool, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	var best uint64
	found := false
	for _, e := range entries {
		if m := okFile.FindStringSubmatch(e.Name()); m != nil {
			n, _ := strconv.ParseUint(m[1], 10, 64)
			if !found || n > best {
				best, found = n, true
			}
		}
	}
	return best, found, nil
}

// readFile maps a vanished file to ErrSaveChanged: Valheim deletes the
// previous save's files once a newer save is complete.
func readFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrSaveChanged
	}
	return b, err
}

func readChunked(dir string, fn func(*ZDO)) (*World, error) {
	n, ok, err := latestChunkedSave(dir)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNoSave
	}
	base := filepath.Join(dir, fmt.Sprintf("_main.%d", n))
	fwl, err := readFile(base + ".fwl2")
	if err != nil {
		return nil, err
	}
	ver, meta, err := readFWL(fwl)
	if err != nil {
		return nil, fmt.Errorf("fwl2: %w", err)
	}
	if ver < VersionChunkedSave || ver > VersionDeepNorth {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedVersion, ver)
	}
	idx, err := readFile(base + ".chunks")
	if err != nil {
		return nil, err
	}
	total, refs, err := readChunkIndex(idx)
	if err != nil {
		return nil, fmt.Errorf("chunks index: %w", err)
	}
	w := &World{Format: FormatChunked, Version: ver, SaveID: fmt.Sprintf("chunked:%d", n), Meta: meta}
	for _, ref := range refs {
		b, err := readFile(filepath.Join(dir, ref.fileName()))
		if err != nil {
			return nil, err
		}
		got, err := readChunkFile(b, fn)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ref.fileName(), err)
		}
		if got != int(ref.ZDOs) {
			return nil, fmt.Errorf("%s: %d zdos, index says %d", ref.fileName(), got, ref.ZDOs)
		}
		w.ZDOCount += got
	}
	if w.ZDOCount != int(total) {
		return nil, fmt.Errorf("save: read %d zdos, index says %d", w.ZDOCount, total)
	}
	db2, err := readFile(base + ".db2")
	if err != nil {
		return nil, err
	}
	nt, zs, err := readDB2(db2)
	if err != nil {
		return nil, fmt.Errorf("db2: %w", err)
	}
	w.NetTime, w.Zones, w.GlobalKeys, w.Locations = nt, zs.Zones, zs.GlobalKeys, zs.Locations
	if _, err := os.Stat(base + ".ok"); err != nil {
		return nil, ErrSaveChanged
	}
	return w, nil
}
```

- [ ] **Step 5: Add the chunked writer** — append to `internal/save/savetest/savetest.go` (and add `"bytes"`, `"compress/gzip"`, `"fmt"`, `"os"`, `"path/filepath"`, `"testing"` to its imports)

```go
type Location struct {
	Name string
	Pos  [3]float32
}

func writeFile(t testing.TB, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func fwl(version int32, name, seedName string) []byte {
	var p zpkg.Writer
	p.I32(version)
	p.Str(name)
	p.Str(seedName)
	p.I32(names.StableHash(seedName))
	p.I64(1)
	p.I32(2)     // world gen version
	p.Bool(true) // needsDB
	p.I32(1)
	p.Str("preset combat_default:deathpenalty_default:resources_default:raids_default:portals_casual")
	if version >= 41 {
		p.I32(0) // empty player roster
	}
	var out zpkg.Writer
	out.ByteArray(p.Bytes())
	return out.Bytes()
}

// WriteChunkedWorld writes a complete 1.0 chunked save <worldsDir>/<worldName>/_main.<n>.*
// with every ZDO in one chunk (1e_1e, size 1, version 1).
func WriteChunkedWorld(t testing.TB, worldsDir, worldName string, n int, seedName string,
	zdos []ZDO, zones [][2]int16, keys []string, locs []Location) {
	t.Helper()
	dir := filepath.Join(worldsDir, worldName)
	base := filepath.Join(dir, fmt.Sprintf("_main.%d", n))

	var chunk zpkg.Writer
	chunk.I16(41)
	chunk.I32(int32(len(zdos)))
	for _, z := range zdos {
		EncodeZDO(&chunk, 41, z)
	}
	writeFile(t, filepath.Join(dir, "1e_1e__1_1.chunk"), chunk.Bytes())

	var idx zpkg.Writer
	idx.I16(41)
	idx.I32(int32(len(zdos)))
	idx.I32(1)
	idx.U16(0x1e1e)
	idx.U8(1)
	idx.U32(1)
	idx.I32(int32(len(zdos)))
	writeFile(t, base+".chunks", idx.Bytes())

	var zp zpkg.Writer
	zp.I32(int32(len(zones)))
	for _, z := range zones {
		zp.Vec2s(z)
	}
	zp.I32(32)
	zp.I32(int32(len(keys)))
	for _, k := range keys {
		zp.Str(k)
	}
	zp.Bool(true)
	zp.I32(int32(len(locs)))
	for _, l := range locs {
		zp.I32(names.StableHash(l.Name))
		zp.Vec3(l.Pos)
		zp.Bool(true)
	}
	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	gw.Write(zp.Bytes())
	gw.Close()
	var db2 zpkg.Writer
	db2.I32(41)
	db2.F64(1800 * 10.5)
	db2.ByteArray(gz.Bytes())
	writeFile(t, base+".db2", db2.Bytes())

	writeFile(t, base+".fwl2", fwl(41, worldName, seedName))
	var ok zpkg.Writer
	ok.I32(41)
	writeFile(t, base+".ok", ok.Bytes())
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `gofmt -l . ; go vet ./... && go test ./internal/save/...`
Expected: `ok`.

- [ ] **Step 7: Commit**

```bash
git add internal/save
git commit -m "feat(save): Valheim 1.0 chunked save reader"
```

---

### Task 5: `save`: the legacy `.db` format

**Files:**
- Create: `internal/save/legacy.go`, `internal/save/legacy_test.go`
- Modify: `internal/save/savetest/savetest.go` (add `WriteLegacyWorld`)

**Interfaces:**
- Consumes: `readFWL`, `DecodeZDO`, `zoneState`, `names.StableHash`.
- Produces: `readLegacy(dbPath, fwlPath string, fn func(*ZDO)) (*World, error)`, `legacySaveID(dbPath string) (string, error)`, and `savetest.WriteLegacyWorld(t testing.TB, worldsDir, worldName, seedName string, zdos []ZDO, zones [][2]int16, keys []string, locs []Location)`.
- Layout for versions 31–39, from `ZNet.LoadOldWorld`, `ZDOMan.Load` and `ZoneSystem.LoadOld`:
  1. Header: `i32 version`, `f64 netTime`, `i64 sessionID`, `u32 nextUid`, `i32 count`.
  2. `count` ZDOs, decoded with `DecodeZDO(version)`.
  3. Zones: `i32 nZones`, then `nZones × (i32 x, i32 y)`, each cast to `int16`.
  4. `i32` (pgw, ignored), then `i32 locationVersion`.
  5. `i32 nKeys`, then `nKeys × Str`.
  6. `Bool locationsGenerated`.
  7. `i32 nLocs`, then `nLocs × {Str name, Vec3, Bool placed}`. The location hash is `StableHash(name)`.
  8. The random-event data that follows is ignored.
- `SaveID` is `legacy:<mtime UnixNano>:<size>` of the `.db` file.
- While `<name>.db.new` exists, a save is being written. `legacySaveID` returns `ErrSaveInProgress`.

- [ ] **Step 1: Write the failing tests** — `internal/save/legacy_test.go`

```go
package save

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/save/savetest"
)

func TestReadLegacySynthetic(t *testing.T) {
	dir := t.TempDir()
	zdos := []savetest.ZDO{
		{Pos: [3]float32{-2182.46, 33.2, 1996.27}, Prefab: "Player_tombstone",
			Longs: map[string]int64{"owner": 100000077}, Strings: map[string]string{"ownerName": "Thordis"}},
		{Pos: [3]float32{5, 6, 7}, Prefab: "bed"},
	}
	savetest.WriteLegacyWorld(t, dir, "Mulennials", "Qm4RtX8vLcL", zdos,
		[][2]int16{{-3, 4}}, []string{"defeated_bonemass"},
		[]savetest.Location{{Name: "Bonemass", Pos: [3]float32{1, 2, 3}}})

	db := filepath.Join(dir, "Mulennials.db")
	var got []ZDO
	w, err := readLegacy(db, filepath.Join(dir, "Mulennials.fwl"), func(z *ZDO) { got = append(got, *z) })
	if err != nil {
		t.Fatal(err)
	}
	if w.Format != FormatLegacy || w.Version != 37 || w.ZDOCount != 2 || len(got) != 2 {
		t.Fatalf("world = %+v", w)
	}
	if got[0].Strings[names.StableHash("ownerName")] != "Thordis" || got[0].Pos[0] != -2182.46 {
		t.Fatalf("zdo = %+v", got[0])
	}
	if w.Meta.SeedName != "Qm4RtX8vLcL" || w.Zones[0] != [2]int16{-3, 4} ||
		w.GlobalKeys[0] != "defeated_bonemass" || names.Name(w.Locations[0].Hash) != "Bonemass" {
		t.Fatalf("world = %+v", w)
	}
	if !strings.HasPrefix(w.SaveID, "legacy:") {
		t.Fatalf("SaveID = %q", w.SaveID)
	}
}

func TestLegacyRejectsOldVersion(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Old.db"), []byte{30, 0, 0, 0}, 0o644)
	savetest.WriteLegacyWorld(t, dir, "Tmp", "x", nil, nil, nil, nil) // just for a valid .fwl
	_, err := readLegacy(filepath.Join(dir, "Old.db"), filepath.Join(dir, "Tmp.fwl"), func(*ZDO) {})
	if err == nil || !strings.Contains(err.Error(), ErrUnsupportedVersion.Error()) {
		t.Fatalf("err = %v, want unsupported version", err)
	}
}

func TestLegacySaveIDInProgress(t *testing.T) {
	dir := t.TempDir()
	savetest.WriteLegacyWorld(t, dir, "W", "x", nil, nil, nil, nil)
	db := filepath.Join(dir, "W.db")
	if _, err := legacySaveID(db); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(db+".new", []byte("partial"), 0o644)
	if _, err := legacySaveID(db); err != ErrSaveInProgress {
		t.Fatalf("err = %v, want ErrSaveInProgress", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/save/ -run Legacy`
Expected: FAIL with `undefined: readLegacy`.

- [ ] **Step 3: Implement** — `internal/save/legacy.go`

```go
package save

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/zpkg"
)

func legacySaveID(dbPath string) (string, error) {
	if _, err := os.Stat(dbPath + ".new"); err == nil {
		return "", ErrSaveInProgress
	}
	fi, err := os.Stat(dbPath)
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNoSave
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("legacy:%d:%d", fi.ModTime().UnixNano(), fi.Size()), nil
}

func readLegacy(dbPath, fwlPath string, fn func(*ZDO)) (*World, error) {
	id, err := legacySaveID(dbPath)
	if err != nil {
		return nil, err
	}
	fwlBytes, err := readFile(fwlPath)
	if err != nil {
		return nil, err
	}
	_, meta, err := readFWL(fwlBytes)
	if err != nil {
		return nil, fmt.Errorf("fwl: %w", err)
	}
	b, err := readFile(dbPath)
	if err != nil {
		return nil, err
	}
	r := zpkg.NewReader(b)
	ver := r.I32()
	if ver < VersionNewSaveFormat || ver >= VersionChunkedSave {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedVersion, ver)
	}
	w := &World{Format: FormatLegacy, Version: ver, SaveID: id, Meta: meta}
	w.NetTime = r.F64()
	r.I64() // session id
	r.U32() // next uid
	n := int(r.I32())
	var z ZDO
	for i := 0; i < n; i++ {
		if err := DecodeZDO(r, ver, &z); err != nil {
			return nil, fmt.Errorf("zdo %d: %w", i, err)
		}
		fn(&z)
	}
	w.ZDOCount = n

	nz := int(r.I32())
	for i := 0; i < nz && r.Err() == nil; i++ {
		x, y := r.I32(), r.I32()
		w.Zones = append(w.Zones, [2]int16{int16(x), int16(y)})
	}
	r.I32() // pgw
	r.I32() // location version
	nk := int(r.I32())
	for i := 0; i < nk && r.Err() == nil; i++ {
		w.GlobalKeys = append(w.GlobalKeys, r.Str())
	}
	r.Bool() // locations generated
	nl := int(r.I32())
	for i := 0; i < nl && r.Err() == nil; i++ {
		name := r.Str()
		w.Locations = append(w.Locations, Location{Hash: names.StableHash(name), Pos: r.Vec3(), Placed: r.Bool()})
	}
	if r.Err() != nil {
		return nil, fmt.Errorf("zone system: %w", r.Err())
	}
	if after, err := legacySaveID(dbPath); err != nil || after != id {
		return nil, ErrSaveChanged
	}
	return w, nil
}
```

- [ ] **Step 4: Add the legacy writer** — append to `internal/save/savetest/savetest.go`

```go
// WriteLegacyWorld writes <worldsDir>/<worldName>.db and .fwl at version 37.
func WriteLegacyWorld(t testing.TB, worldsDir, worldName, seedName string,
	zdos []ZDO, zones [][2]int16, keys []string, locs []Location) {
	t.Helper()
	var db zpkg.Writer
	db.I32(37)
	db.F64(1800 * 42.25)
	db.I64(0)
	db.U32(0)
	db.I32(int32(len(zdos)))
	for _, z := range zdos {
		EncodeZDO(&db, 37, z)
	}
	db.I32(int32(len(zones)))
	for _, z := range zones {
		db.I32(int32(z[0]))
		db.I32(int32(z[1]))
	}
	db.I32(0)  // pgw
	db.I32(32) // location version
	db.I32(int32(len(keys)))
	for _, k := range keys {
		db.Str(k)
	}
	db.Bool(true)
	db.I32(int32(len(locs)))
	for _, l := range locs {
		db.Str(l.Name)
		db.Vec3(l.Pos)
		db.Bool(true)
	}
	db.I32(0) // random events: ignored by the reader
	writeFile(t, filepath.Join(worldsDir, worldName+".db"), db.Bytes())
	writeFile(t, filepath.Join(worldsDir, worldName+".fwl"), fwl(37, worldName, seedName))
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -l . ; go vet ./... && go test ./internal/save/...`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/save
git commit -m "feat(save): legacy .db world reader (versions 31-39)"
```

---

### Task 6: `save.Read`, `save.LatestSave` and the golden tests

**Files:**
- Modify: `internal/save/world.go`
- Create: `internal/save/golden_test.go`, `hack/pull-golden.sh`

**Interfaces:**
- Produces:
  - `save.LatestSave(worldsDir, worldName string) (id string, format Format, err error)`. The chunked directory `<worldsDir>/<worldName>/` containing a `_main.N.ok` wins and gives `"chunked:N"`. Otherwise it falls back to `<worldsDir>/<worldName>.db` via `legacySaveID`. It returns `ErrNoSave` when neither exists.
  - `save.Read(worldsDir, worldName string, fn func(*ZDO)) (*World, error)`, which picks the format the same way. `fn` receives a pointer that is reused for every ZDO, so callers must copy anything they keep.
- Golden layout (gitignored): `testdata-golden/chunked/MuleVikings/…` and `testdata-golden/legacy/Mulennials.{db,fwl}`.

- [ ] **Step 1: Write the pull script** — `hack/pull-golden.sh`

```bash
#!/usr/bin/env bash
# Copies real world saves from the cluster for golden tests (read-only).
set -euo pipefail
: "${KUBECONFIG:=$HOME/.kube/cloudcluster.yaml}"; export KUBECONFIG
ROOT=$(cd "$(dirname "$0")/.." && pwd)/testdata-golden
mkdir -p "$ROOT/chunked" "$ROOT/legacy"
kubectl -n gameservers exec deploy/mulevikings-valheim -c valheim -- \
  tar cf - -C /config/worlds_local MuleVikings | tar xf - -C "$ROOT/chunked"
for f in Mulennials.db Mulennials.fwl; do
  kubectl -n gameservers exec deploy/mulevikings-old-valheim -c valheim -- \
    cat "/config/worlds_local/$f" > "$ROOT/legacy/$f"
done
du -sh "$ROOT"/*
```

Run: `chmod +x hack/pull-golden.sh && hack/pull-golden.sh`
Expected: about 18M for `chunked` and 81M for `legacy`. If `Mulennials.db` is being saved at that moment, the golden test fails with `ErrSaveChanged`. Re-run the script.

- [ ] **Step 2: Write the failing tests** — `internal/save/golden_test.go`

```go
package save

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/save/savetest"
)

func golden(t *testing.T, sub string) string {
	t.Helper()
	p := filepath.Join("..", "..", "testdata-golden", sub)
	if _, err := os.Stat(p); err != nil {
		t.Skip("golden data missing; run hack/pull-golden.sh")
	}
	return p
}

func TestGoldenChunked(t *testing.T) {
	dir := golden(t, "chunked")
	unknown := map[int32]bool{}
	portals := map[string]int{}
	w, err := Read(dir, "MuleVikings", func(z *ZDO) {
		if _, ok := names.Lookup(z.Prefab); !ok {
			unknown[z.Prefab] = true
		}
		if names.Name(z.Prefab) == "portal_wood" {
			portals[z.Strings[names.StableHash("tag")]]++
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.Format != FormatChunked || w.ZDOCount < 400000 || len(unknown) != 0 {
		t.Fatalf("format=%s zdos=%d unknown=%d", w.Format, w.ZDOCount, len(unknown))
	}
	if w.Meta.SeedName != "FjordSeed" || len(w.Zones) < 5000 || len(w.Locations) < 10000 {
		t.Fatalf("meta=%+v zones=%d locs=%d", w.Meta, len(w.Zones), len(w.Locations))
	}
	if len(portals) < 8 {
		t.Fatalf("portal tags = %v", portals)
	}
}

func TestGoldenLegacy(t *testing.T) {
	dir := golden(t, "legacy")
	unknown := map[int32]bool{}
	w, err := Read(dir, "Mulennials", func(z *ZDO) {
		if _, ok := names.Lookup(z.Prefab); !ok {
			unknown[z.Prefab] = true
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.Format != FormatLegacy || w.Version != 37 || w.ZDOCount < 1000000 || w.Meta.SeedName != "Qm4RtX8vLcL" {
		t.Fatalf("world = %+v", w)
	}
	if len(unknown) > 0 {
		t.Logf("%d unknown prefab hashes (content removed in 1.0?)", len(unknown))
	}
	if len(w.Locations) < 5000 || len(w.Zones) < 1000 {
		t.Fatalf("zones=%d locs=%d", len(w.Zones), len(w.Locations))
	}
}

func TestReadPrefersChunkedAndLatestSave(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := LatestSave(dir, "W"); err != ErrNoSave {
		t.Fatalf("err = %v, want ErrNoSave", err)
	}
	savetest.WriteLegacyWorld(t, dir, "W", "x", nil, nil, nil, nil)
	if _, f, err := LatestSave(dir, "W"); err != nil || f != FormatLegacy {
		t.Fatalf("got %s %v", f, err)
	}
	savetest.WriteChunkedWorld(t, dir, "W", 3, "x", []savetest.ZDO{{Prefab: "bed"}}, nil, nil, nil)
	id, f, err := LatestSave(dir, "W")
	if err != nil || f != FormatChunked || id != "chunked:3" {
		t.Fatalf("got %s %s %v", id, f, err)
	}
	w, err := Read(dir, "W", func(*ZDO) {})
	if err != nil || w.Format != FormatChunked {
		t.Fatalf("Read: %v %+v", err, w)
	}
}
```

The spike showed `mulevikings-old` has a 1.8M-ZDO world. The `1000000` floor is deliberately loose.

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/save/ -run 'Golden|Prefers'`
Expected: FAIL with `undefined: Read`.

- [ ] **Step 4: Implement** — append to `internal/save/world.go` (add imports `"fmt"`, `"path/filepath"`)

```go
// LatestSave reports the newest complete save's identity without reading it.
func LatestSave(worldsDir, worldName string) (string, Format, error) {
	n, ok, err := latestChunkedSave(filepath.Join(worldsDir, worldName))
	if err != nil {
		return "", "", err
	}
	if ok {
		return fmt.Sprintf("chunked:%d", n), FormatChunked, nil
	}
	id, err := legacySaveID(filepath.Join(worldsDir, worldName+".db"))
	if err != nil {
		return "", "", err
	}
	return id, FormatLegacy, nil
}

// Read streams every ZDO of the newest save to fn and returns the world
// metadata. fn's argument is reused between calls.
func Read(worldsDir, worldName string, fn func(*ZDO)) (*World, error) {
	_, f, err := LatestSave(worldsDir, worldName)
	if err != nil {
		return nil, err
	}
	if f == FormatChunked {
		return readChunked(filepath.Join(worldsDir, worldName), fn)
	}
	return readLegacy(filepath.Join(worldsDir, worldName+".db"), filepath.Join(worldsDir, worldName+".fwl"), fn)
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/save/... -v -run 'Golden|Prefers' 2>&1 | tail -20 && go test ./...`
Expected: PASS; the golden tests pass rather than skip because the data was pulled in Step 1.

- [ ] **Step 6: Commit**

```bash
git add internal/save hack/pull-golden.sh
git commit -m "feat(save): format detection, Read/LatestSave, golden tests on live saves"
```

---

### Task 7: `extract`: turning ZDOs into a snapshot

**Files:**
- Create: `internal/extract/snapshot.go`, `internal/extract/tables.go`, `internal/extract/bases.go`, `internal/extract/extract.go`, `internal/extract/extract_test.go`, `internal/extract/bases_test.go`, `internal/extract/golden_test.go`

**Interfaces:**
- Consumes: `save.ZDO`, `save.World`, `names`.
- Produces:
  ```go
  func New() *Extractor
  func (e *Extractor) Add(z *save.ZDO)   // safe to call with a reused pointer
  func (e *Extractor) Finish(w *save.World, serverID string, savedAt time.Time) *Snapshot
  ```
  and the JSON types below (field names are the wire contract for Plan 4):
  ```go
  type Snapshot struct {
      ServerID string `json:"serverId"`; SaveID string `json:"saveId"`; SavedAt time.Time `json:"savedAt"`
      Format string `json:"format"`; World WorldInfo `json:"world"`; GlobalKeys []string `json:"globalKeys"`
      Bosses []Boss `json:"bosses"`; ExploredZones [][2]int16 `json:"exploredZones"`
      Locations []Marker `json:"locations"`; Markers []Marker `json:"markers"`
      Bases []Base `json:"bases"`; Players []Player `json:"players"`; Stats Stats `json:"stats"`
  }
  type WorldInfo struct { Name, SeedName string; Seed int32; GenVersion int32; NetTime float64; Day int; Modifiers map[string]string; Flags []string }  // json: name, seedName, seed, genVersion, netTime, day, modifiers, flags
  type Boss struct { Key, Name string; Defeated bool }                   // key, name, defeated
  type Marker struct { ID, Kind string; X, Y, Z float32; Label, Owner, Species, Type, Pair string } // id, kind, x, y, z, label?, owner?, species?, type?, pair?
  type Base struct { ID, Name string; X, Z, Radius float32; Pieces int; Builders []Builder }
  type Builder struct { ID int64; Name string; Pieces int }
  type Player struct { ID int64; Name string }
  type Stats struct { ZDOs, Pieces, UnknownPrefabs int }
  ```
- Rules:
  - **Markers.** A prefab whose name starts with `portal` becomes `portal`, with `label` set to the `tag` string. Portals are paired when exactly two share a tag, including an empty tag, and each gets `pair` set to the other's ID. Prefab `bed`, or any prefab starting `piece_bed`, becomes `bed` with `owner` from `ownerName`. A ZDO with a `TamedName` string, or with `tamed` int 1, becomes `tame`, with `species` set to the prefab name and `label` set to the TamedName. `Player_tombstone` becomes `tombstone` with `owner` from `ownerName`. Prefab `sign` becomes `sign` with `label` from `text`. IDs are `<kind>-<n>`, numbered from 1 per kind in the order the ZDOs arrive.
  - **Locations.** Only types in the tables become location markers, with `type` set to the prefab name and `label` to the display name. Everything else is dropped.
  - **Players.** Beds and tombstones with a non-zero `owner` long and a non-empty `ownerName` map ID to name. The last name seen wins.
  - **Bases.** Every ZDO with a non-zero `creator` long counts as a piece. They are clustered as in the spec: 16 m cells, occupied cells within Chebyshev distance 2 are connected, and a base needs at least 40 pieces. A base is named `"<top builder name>'s base"`, or `"Base"` when the top builder is unknown. Bases are sorted by pieces descending, with IDs `base-1`, `base-2` and so on.
  - **Bosses.** The eight keys come in order from `bossKeys`, and `defeated` is true when the key is present in `GlobalKeys`.
  - **World.** `Day = int(NetTime / 1800)`. Each starting key of the form `preset a_b:c_d` fills `Modifiers` as `{"a": "b", "c": "d"}`, with later presets overriding earlier ones. Other starting keys go into `Flags`, deduplicated.

- [ ] **Step 1: Write the tables** — `internal/extract/tables.go`

```go
package extract

// Location prefabs that become atlas markers, from the spike's location list.
var bossAltars = map[string]string{
	"Eikthyrnir":                    "Eikthyr",
	"GDKing":                        "The Elder",
	"Bonemass":                      "Bonemass",
	"Dragonqueen":                   "Moder",
	"GoblinKing":                    "Yagluth",
	"Mistlands_DvergrBossEntrance1": "The Queen",
	"FaderLocation":                 "Fader",
	"DN_Bossroom":                   "Writhan",
}

var traders = map[string]string{
	"Vendor_BlackForest": "Haldor",
	"Hildir_camp":        "Hildir",
	"BogWitch_Camp":      "Bog Witch",
}

var dungeons = map[string]string{
	"Crypt2":                "Burial chambers",
	"Crypt3":                "Burial chambers",
	"Crypt4":                "Burial chambers",
	"SunkenCrypt4":          "Sunken crypt",
	"TrollCave02":           "Troll cave",
	"MountainCave02":        "Frost cave",
	"BearCave":              "Bear cave",
	"Hildir_cave":           "Howling cavern",
	"Hildir_crypt":          "Smouldering tomb",
	"Hildir_plainsfortress": "Sealed tower",
	"GoblinCamp2":           "Fuling village",
}

// Boss progression, in game order, keyed by the global key set on defeat.
var bossKeys = []struct{ Key, Name string }{
	{"defeated_eikthyr", "Eikthyr"},
	{"defeated_gdking", "The Elder"},
	{"defeated_bonemass", "Bonemass"},
	{"defeated_dragon", "Moder"},
	{"defeated_goblinking", "Yagluth"},
	{"defeated_queen", "The Queen"},
	{"defeated_fader", "Fader"},
	{"defeated_writhan", "Writhan"},
}
```

- [ ] **Step 2: Write the snapshot types** — `internal/extract/snapshot.go`

```go
// Package extract turns a stream of save ZDOs into an atlas Snapshot.
package extract

import "time"

type Snapshot struct {
	ServerID      string     `json:"serverId"`
	SaveID        string     `json:"saveId"`
	SavedAt       time.Time  `json:"savedAt"`
	Format        string     `json:"format"`
	World         WorldInfo  `json:"world"`
	GlobalKeys    []string   `json:"globalKeys"`
	Bosses        []Boss     `json:"bosses"`
	ExploredZones [][2]int16 `json:"exploredZones"`
	Locations     []Marker   `json:"locations"`
	Markers       []Marker   `json:"markers"`
	Bases         []Base     `json:"bases"`
	Players       []Player   `json:"players"`
	Stats         Stats      `json:"stats"`
}

type WorldInfo struct {
	Name       string            `json:"name"`
	SeedName   string            `json:"seedName"`
	Seed       int32             `json:"seed"`
	GenVersion int32             `json:"genVersion"`
	NetTime    float64           `json:"netTime"`
	Day        int               `json:"day"`
	Modifiers  map[string]string `json:"modifiers"`
	Flags      []string          `json:"flags"`
}

type Boss struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Defeated bool   `json:"defeated"`
}

type Marker struct {
	ID      string  `json:"id"`
	Kind    string  `json:"kind"`
	X       float32 `json:"x"`
	Y       float32 `json:"y"`
	Z       float32 `json:"z"`
	Label   string  `json:"label,omitempty"`
	Owner   string  `json:"owner,omitempty"`
	Species string  `json:"species,omitempty"`
	Type    string  `json:"type,omitempty"`
	Pair    string  `json:"pair,omitempty"`
}

type Base struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	X        float32   `json:"x"`
	Z        float32   `json:"z"`
	Radius   float32   `json:"radius"`
	Pieces   int       `json:"pieces"`
	Builders []Builder `json:"builders"`
}

type Builder struct {
	ID     int64  `json:"id"`
	Name   string `json:"name,omitempty"`
	Pieces int    `json:"pieces"`
}

type Player struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Stats struct {
	ZDOs           int `json:"zdos"`
	Pieces         int `json:"pieces"`
	UnknownPrefabs int `json:"unknownPrefabs"`
}
```

- [ ] **Step 3: Write the failing tests** — `internal/extract/bases_test.go`

```go
package extract

import "testing"

func grid(x0, z0 float32, n int, creator int64) []piece {
	out := make([]piece, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, piece{X: x0 + float32(i%10)*2, Z: z0 + float32(i/10)*2, Creator: creator})
	}
	return out
}

func TestClusterBases(t *testing.T) {
	var ps []piece
	ps = append(ps, grid(0, 0, 60, 1)...)      // base A: 60 pieces by 1
	ps = append(ps, grid(40, 0, 20, 2)...)     // 20 pieces 22 m east of A's edge: joins A (gap <= 32 m)
	ps = append(ps, grid(1000, 1000, 45, 3)...) // base B: 45 pieces by 3
	ps = append(ps, grid(-3000, 0, 10, 4)...)   // too small: dropped
	bases := clusterBases(ps, map[int64]string{1: "Thorgerdr"})
	if len(bases) != 2 {
		t.Fatalf("got %d bases, want 2: %+v", len(bases), bases)
	}
	a, b := bases[0], bases[1]
	if a.Pieces != 80 || a.ID != "base-1" || a.Name != "Thorgerdr's base" || len(a.Builders) != 2 || a.Builders[0].ID != 1 || a.Builders[0].Pieces != 60 {
		t.Fatalf("base A = %+v", a)
	}
	if b.Pieces != 45 || b.Name != "Base" || b.ID != "base-2" {
		t.Fatalf("base B = %+v", b)
	}
	if a.Radius <= 0 || a.X < 0 || a.X > 60 {
		t.Fatalf("base A geometry = %+v", a)
	}
}
```

`internal/extract/extract_test.go`

```go
package extract

import (
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/save"
)

func h(s string) int32 { return names.StableHash(s) }

func z(prefab string, pos [3]float32) *save.ZDO {
	return &save.ZDO{Prefab: h(prefab), Pos: pos}
}

func TestExtractMarkersPlayersBosses(t *testing.T) {
	e := New()
	p1 := z("portal_wood", [3]float32{1, 2, 3})
	p1.Strings = map[int32]string{h("tag"): "Trader"}
	p2 := z("portal_wood", [3]float32{4, 5, 6})
	p2.Strings = map[int32]string{h("tag"): "Trader"}
	p3 := z("portal_wood", [3]float32{7, 8, 9})
	p3.Strings = map[int32]string{h("tag"): "lonely"}
	bed := z("bed", [3]float32{10, 0, 10})
	bed.Longs = map[int32]int64{h("owner"): 1000000099}
	bed.Strings = map[int32]string{h("ownerName"): "Sigrun"}
	goblinBed := z("goblin_bed", [3]float32{0, 0, 0})
	wolf := z("Wolf", [3]float32{20, 0, 20})
	wolf.Ints = map[int32]int32{h("tamed"): 1}
	wolf.Strings = map[int32]string{h("TamedName"): "Skollr"}
	wildWolf := z("Wolf", [3]float32{0, 0, 0})
	tomb := z("Player_tombstone", [3]float32{-2182, 33, 1996})
	tomb.Strings = map[int32]string{h("ownerName"): "Thordis"}
	unknown := &save.ZDO{Prefab: 12345}
	for _, zz := range []*save.ZDO{p1, p2, p3, bed, goblinBed, wolf, wildWolf, tomb, unknown} {
		e.Add(zz)
	}
	w := &save.World{
		Format: save.FormatChunked, SaveID: "chunked:9", NetTime: 1800*278 + 5, ZDOCount: 9,
		Meta: save.Meta{Name: "MuleVikings", SeedName: "FjordSeed", Seed: -1032944128, GenVersion: 2,
			StartingKeys: []string{"teleportall", "preset combat_default:portals_casual", "teleportall"}},
		Zones:      [][2]int16{{0, 0}},
		GlobalKeys: []string{"defeated_eikthyr", "defeated_writhan"},
		Locations: []save.Location{
			{Hash: h("Eikthyrnir"), Pos: [3]float32{100, 30, 200}},
			{Hash: h("Crypt2"), Pos: [3]float32{1, 1, 1}},
			{Hash: h("Runestone_Meadows"), Pos: [3]float32{2, 2, 2}},
		},
	}
	s := e.Finish(w, "mulevikings", time.Unix(1700000000, 0).UTC())

	kinds := map[string][]Marker{}
	for _, m := range s.Markers {
		kinds[m.Kind] = append(kinds[m.Kind], m)
	}
	if len(kinds["portal"]) != 3 || len(kinds["bed"]) != 1 || len(kinds["tame"]) != 1 || len(kinds["tombstone"]) != 1 {
		t.Fatalf("markers = %+v", s.Markers)
	}
	pa, pb, pc := kinds["portal"][0], kinds["portal"][1], kinds["portal"][2]
	if pa.Pair != pb.ID || pb.Pair != pa.ID || pc.Pair != "" || pa.Label != "Trader" {
		t.Fatalf("portal pairing = %+v %+v %+v", pa, pb, pc)
	}
	if tm := kinds["tame"][0]; tm.Species != "Wolf" || tm.Label != "Skollr" {
		t.Fatalf("tame = %+v", tm)
	}
	if kinds["bed"][0].Owner != "Sigrun" || kinds["tombstone"][0].Owner != "Thordis" {
		t.Fatal("owners not extracted")
	}
	if len(s.Players) != 1 || s.Players[0].ID != 1000000099 || s.Players[0].Name != "Sigrun" {
		t.Fatalf("players = %+v", s.Players)
	}
	if len(s.Locations) != 2 || s.Locations[0].Kind != "boss_altar" || s.Locations[0].Label != "Eikthyr" || s.Locations[1].Kind != "dungeon" {
		t.Fatalf("locations = %+v", s.Locations)
	}
	if len(s.Bosses) != 8 || !s.Bosses[0].Defeated || s.Bosses[1].Defeated || !s.Bosses[7].Defeated {
		t.Fatalf("bosses = %+v", s.Bosses)
	}
	if s.World.Day != 278 || s.World.Modifiers["combat"] != "default" || s.World.Modifiers["portals"] != "casual" ||
		len(s.World.Flags) != 1 || s.World.Flags[0] != "teleportall" {
		t.Fatalf("world = %+v", s.World)
	}
	if s.Stats.UnknownPrefabs != 1 || s.Stats.ZDOs != 9 || s.ServerID != "mulevikings" || s.SaveID != "chunked:9" || s.Format != "chunked" {
		t.Fatalf("snapshot header/stats = %+v", s)
	}
}

func TestAddCopiesReusedPointer(t *testing.T) {
	e := New()
	zz := z("portal_wood", [3]float32{1, 0, 1})
	zz.Strings = map[int32]string{h("tag"): "a"}
	e.Add(zz)
	*zz = save.ZDO{Prefab: h("portal_wood"), Pos: [3]float32{9, 0, 9}, Strings: map[int32]string{h("tag"): "b"}}
	e.Add(zz)
	s := e.Finish(&save.World{}, "x", time.Now())
	if s.Markers[0].Label != "a" || s.Markers[0].X != 1 {
		t.Fatalf("first marker was overwritten: %+v", s.Markers[0])
	}
}
```

- [ ] **Step 4: Run to verify it fails**

Run: `go test ./internal/extract/`
Expected: FAIL with `undefined: piece` / `undefined: New`.

- [ ] **Step 5: Implement base clustering** — `internal/extract/bases.go`

```go
package extract

import (
	"fmt"
	"math"
	"sort"
)

type piece struct {
	X, Z    float32
	Creator int64
}

const (
	cellSize  = 16
	linkCells = 2 // occupied cells within this Chebyshev distance join (<= 32 m gap)
	minPieces = 40
)

type cell struct{ X, Z int32 }

func clusterBases(ps []piece, playerNames map[int64]string) []Base {
	cells := map[cell][]int{}
	for i, p := range ps {
		c := cell{int32(math.Floor(float64(p.X) / cellSize)), int32(math.Floor(float64(p.Z) / cellSize))}
		cells[c] = append(cells[c], i)
	}
	seen := map[cell]bool{}
	var bases []Base
	for start := range cells {
		if seen[start] {
			continue
		}
		seen[start] = true
		queue := []cell{start}
		var members []int
		for len(queue) > 0 {
			c := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			members = append(members, cells[c]...)
			for dx := int32(-linkCells); dx <= linkCells; dx++ {
				for dz := int32(-linkCells); dz <= linkCells; dz++ {
					n := cell{c.X + dx, c.Z + dz}
					if _, ok := cells[n]; ok && !seen[n] {
						seen[n] = true
						queue = append(queue, n)
					}
				}
			}
		}
		if len(members) >= minPieces {
			bases = append(bases, makeBase(ps, members, playerNames))
		}
	}
	sort.Slice(bases, func(i, j int) bool {
		if bases[i].Pieces != bases[j].Pieces {
			return bases[i].Pieces > bases[j].Pieces
		}
		return bases[i].X < bases[j].X
	})
	for i := range bases {
		bases[i].ID = fmt.Sprintf("base-%d", i+1)
	}
	return bases
}

func makeBase(ps []piece, members []int, playerNames map[int64]string) Base {
	var sx, sz float64
	counts := map[int64]int{}
	for _, i := range members {
		sx += float64(ps[i].X)
		sz += float64(ps[i].Z)
		counts[ps[i].Creator]++
	}
	cx, cz := sx/float64(len(members)), sz/float64(len(members))
	var r float64
	for _, i := range members {
		r = math.Max(r, math.Hypot(float64(ps[i].X)-cx, float64(ps[i].Z)-cz))
	}
	b := Base{X: float32(cx), Z: float32(cz), Radius: float32(r), Pieces: len(members)}
	for id, n := range counts {
		b.Builders = append(b.Builders, Builder{ID: id, Name: playerNames[id], Pieces: n})
	}
	sort.Slice(b.Builders, func(i, j int) bool {
		if b.Builders[i].Pieces != b.Builders[j].Pieces {
			return b.Builders[i].Pieces > b.Builders[j].Pieces
		}
		return b.Builders[i].ID < b.Builders[j].ID
	})
	b.Name = "Base"
	if top := b.Builders[0].Name; top != "" {
		b.Name = top + "'s base"
	}
	return b
}
```

- [ ] **Step 6: Implement the extractor** — `internal/extract/extract.go`

```go
package extract

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jumpingmushroom/farsight/internal/names"
	"github.com/jumpingmushroom/farsight/internal/save"
)

var (
	kTag       = names.StableHash("tag")
	kOwner     = names.StableHash("owner")
	kOwnerName = names.StableHash("ownerName")
	kTamed     = names.StableHash("tamed")
	kTamedName = names.StableHash("TamedName")
	kText      = names.StableHash("text")
	kCreator   = names.StableHash("creator")
)

type Extractor struct {
	markers []Marker
	counts  map[string]int
	pieces  []piece
	players map[int64]string
	unknown map[int32]bool
}

func New() *Extractor {
	return &Extractor{counts: map[string]int{}, players: map[int64]string{}, unknown: map[int32]bool{}}
}

func (e *Extractor) mark(kind string, z *save.ZDO) *Marker {
	e.counts[kind]++
	e.markers = append(e.markers, Marker{
		ID: fmt.Sprintf("%s-%d", kind, e.counts[kind]), Kind: kind,
		X: z.Pos[0], Y: z.Pos[1], Z: z.Pos[2],
	})
	return &e.markers[len(e.markers)-1]
}

func (e *Extractor) owner(z *save.ZDO) string {
	name := z.Strings[kOwnerName]
	if id := z.Longs[kOwner]; id != 0 && name != "" {
		e.players[id] = name
	}
	return name
}

// Add inspects one ZDO. It copies what it keeps, so z may be reused.
func (e *Extractor) Add(z *save.ZDO) {
	name, ok := names.Lookup(z.Prefab)
	if !ok {
		e.unknown[z.Prefab] = true
	}
	if id := z.Longs[kCreator]; id != 0 {
		e.pieces = append(e.pieces, piece{X: z.Pos[0], Z: z.Pos[2], Creator: id})
	}
	switch {
	case strings.HasPrefix(name, "portal"):
		e.mark("portal", z).Label = z.Strings[kTag]
	case name == "bed" || strings.HasPrefix(name, "piece_bed"):
		e.mark("bed", z).Owner = e.owner(z)
	case name == "Player_tombstone":
		e.mark("tombstone", z).Owner = e.owner(z)
	case name == "sign":
		e.mark("sign", z).Label = z.Strings[kText]
	case z.Strings[kTamedName] != "" || z.Ints[kTamed] == 1:
		m := e.mark("tame", z)
		m.Species, m.Label = name, z.Strings[kTamedName]
	}
}

func (e *Extractor) Finish(w *save.World, serverID string, savedAt time.Time) *Snapshot {
	s := &Snapshot{
		ServerID: serverID, SaveID: w.SaveID, SavedAt: savedAt, Format: string(w.Format),
		GlobalKeys: w.GlobalKeys, ExploredZones: w.Zones, Markers: e.markers,
		World: worldInfo(w),
		Stats: Stats{ZDOs: w.ZDOCount, Pieces: len(e.pieces), UnknownPrefabs: len(e.unknown)},
	}
	pairPortals(s.Markers)
	for i, l := range w.Locations {
		name := names.Name(l.Hash)
		kind, label := "", ""
		if v, ok := bossAltars[name]; ok {
			kind, label = "boss_altar", v
		} else if v, ok := traders[name]; ok {
			kind, label = "trader", v
		} else if v, ok := dungeons[name]; ok {
			kind, label = "dungeon", v
		} else {
			continue
		}
		s.Locations = append(s.Locations, Marker{
			ID: fmt.Sprintf("loc-%d", i+1), Kind: kind, Type: name, Label: label,
			X: l.Pos[0], Y: l.Pos[1], Z: l.Pos[2],
		})
	}
	keys := map[string]bool{}
	for _, k := range w.GlobalKeys {
		keys[k] = true
	}
	for _, b := range bossKeys {
		s.Bosses = append(s.Bosses, Boss{Key: b.Key, Name: b.Name, Defeated: keys[b.Key]})
	}
	for id, name := range e.players {
		s.Players = append(s.Players, Player{ID: id, Name: name})
	}
	sort.Slice(s.Players, func(i, j int) bool { return s.Players[i].Name < s.Players[j].Name })
	s.Bases = clusterBases(e.pieces, e.players)
	return s
}

func pairPortals(ms []Marker) {
	byTag := map[string][]int{}
	for i, m := range ms {
		if m.Kind == "portal" {
			byTag[m.Label] = append(byTag[m.Label], i)
		}
	}
	for _, idx := range byTag {
		if len(idx) == 2 {
			ms[idx[0]].Pair, ms[idx[1]].Pair = ms[idx[1]].ID, ms[idx[0]].ID
		}
	}
}

func worldInfo(w *save.World) WorldInfo {
	wi := WorldInfo{
		Name: w.Meta.Name, SeedName: w.Meta.SeedName, Seed: w.Meta.Seed, GenVersion: w.Meta.GenVersion,
		NetTime: w.NetTime, Day: int(w.NetTime / 1800), Modifiers: map[string]string{},
	}
	seenFlag := map[string]bool{}
	for _, k := range w.Meta.StartingKeys {
		if rest, ok := strings.CutPrefix(k, "preset "); ok {
			for _, part := range strings.Split(rest, ":") {
				if a, b, ok := strings.Cut(part, "_"); ok {
					wi.Modifiers[a] = b
				}
			}
			continue
		}
		if !seenFlag[k] {
			seenFlag[k] = true
			wi.Flags = append(wi.Flags, k)
		}
	}
	return wi
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `gofmt -l . ; go vet ./... && go test ./internal/extract/`
Expected: `ok`.

- [ ] **Step 8: Write the golden test** — `internal/extract/golden_test.go`

```go
package extract

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jumpingmushroom/farsight/internal/save"
)

func TestGoldenMuleVikings(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata-golden", "chunked")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("golden data missing; run hack/pull-golden.sh")
	}
	e := New()
	w, err := save.Read(dir, "MuleVikings", e.Add)
	if err != nil {
		t.Fatal(err)
	}
	s := e.Finish(w, "mulevikings", time.Now().UTC())
	count := map[string]int{}
	paired := 0
	for _, m := range s.Markers {
		count[m.Kind]++
		if m.Kind == "portal" && m.Pair != "" {
			paired++
		}
	}
	t.Logf("markers=%v bases=%d players=%d locations=%d", count, len(s.Bases), len(s.Players), len(s.Locations))
	if count["portal"] < 10 || paired < 10 || count["bed"] < 5 || count["tame"] < 1 {
		t.Fatalf("too few markers: %v (paired portals %d)", count, paired)
	}
	if len(s.Bases) < 2 || s.Bases[0].Name == "Base" {
		t.Fatalf("bases = %+v", s.Bases)
	}
	if len(s.Players) < 5 || s.Stats.UnknownPrefabs != 0 || s.World.SeedName != "FjordSeed" {
		t.Fatalf("players=%d unknown=%d seed=%q", len(s.Players), s.Stats.UnknownPrefabs, s.World.SeedName)
	}
	altars := 0
	for _, l := range s.Locations {
		if l.Kind == "boss_altar" {
			altars++
		}
	}
	if altars < 8 || s.World.Modifiers["portals"] != "casual" {
		t.Fatalf("altars=%d modifiers=%v", altars, s.World.Modifiers)
	}
}
```

Run: `go test ./internal/extract/ -run Golden -v`
Expected: PASS, with a log line like `markers=map[bed:14 portal:16 tame:12 tombstone:1] bases=… players=8`.

- [ ] **Step 9: Commit**

```bash
git add internal/extract
git commit -m "feat(extract): snapshot extraction with markers, locations, bosses, bases"
```

---

### Task 8: The dev CLI `farsight-snapshot`

**Files:**
- Create: `cmd/farsight-snapshot/main.go`

**Interfaces:**
- Consumes: `save.Read`, `extract.New`, `Extractor.Add`, `Extractor.Finish`.
- Produces: `farsight-snapshot -worlds DIR -world NAME [-server ID]`, which prints the snapshot JSON (indented) to stdout and timing to stderr.

- [ ] **Step 1: Implement** — `cmd/farsight-snapshot/main.go`

```go
// Command farsight-snapshot prints the atlas snapshot of a world save as JSON.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/save"
)

func main() {
	worlds := flag.String("worlds", ".", "worlds_local directory")
	world := flag.String("world", "", "world name (required)")
	server := flag.String("server", "local", "server id to stamp on the snapshot")
	flag.Parse()
	if *world == "" {
		flag.Usage()
		os.Exit(2)
	}
	start := time.Now()
	e := extract.New()
	w, err := save.Read(*worlds, *world, e.Add)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	s := e.Finish(w, *server, time.Now().UTC())
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "%s: %d zdos, %d markers, %d bases in %s\n",
		w.SaveID, w.ZDOCount, len(s.Markers), len(s.Bases), time.Since(start).Round(time.Millisecond))
}
```

- [ ] **Step 2: Run it on both golden worlds**

```bash
go run ./cmd/farsight-snapshot -worlds testdata-golden/chunked -world MuleVikings > /tmp/mv.json
go run ./cmd/farsight-snapshot -worlds testdata-golden/legacy -world Mulennials > /tmp/mo.json
```
Expected on stderr: one line per world, e.g. `chunked:851: 476157 zdos, 43 markers, … in 1.2s`, and the legacy world in under about 10 s. Check `/tmp/mv.json` by eye: `world.seedName` is `FjordSeed`, and there are 8 bosses with Eikthyr, The Elder and Writhan defeated. Record both timings in the commit message body; Plan 5 sizes the sidecar limits from them.

- [ ] **Step 3: Commit**

```bash
git add cmd/farsight-snapshot
git commit -m "feat(cli): farsight-snapshot dev tool"
```

---

### Task 9: `agent`: the save watcher and snapshot pusher

**Files:**
- Create: `internal/agent/agent.go`, `internal/agent/agent_test.go`, `cmd/farsight-agent/main.go`

**Interfaces:**
- Consumes: `save.LatestSave`, `save.Read`, `save.ErrNoSave`, `save.ErrSaveInProgress`, `save.ErrSaveChanged`, `extract`.
- Produces:
  ```go
  type Config struct { WorldsDir, WorldName, ServerID, URL, Token string; Poll time.Duration }
  func New(cfg Config, log *slog.Logger) *Agent
  func (a *Agent) Tick(ctx context.Context) error   // one poll; exported for tests
  func (a *Agent) Run(ctx context.Context) error    // Tick every cfg.Poll until ctx is done
  ```
- Behaviour of `Tick`:
  1. Call `LatestSave`. `ErrNoSave` and `ErrSaveInProgress` are logged at debug level and return nil.
  2. If the id equals the last id sent successfully, return nil.
  3. A **legacy** id must be seen on two consecutive ticks before it is processed. Store it as `candidate` and return. A chunked id is processed at once, because `.ok` is written last.
  4. `save.Read` into a fresh extractor. `ErrSaveChanged` is logged at info level and returns nil; the next tick retries.
  5. Marshal the snapshot to JSON, gzip it, and POST it as described in the Global Constraints, with a 60 s timeout.
  6. On 2xx, record `lastSent = id`. Any other status or error returns an error and `lastSent` is unchanged, so the next tick retries.
- `SavedAt` is the time of reading in UTC.
- `Run` logs errors from `Tick` and keeps going.
- Env for `cmd/farsight-agent`: `FARSIGHT_WORLDS_DIR` (default `/worlds/worlds_local`), `WORLD_NAME` (required), `FARSIGHT_SERVER_ID` (required), `FARSIGHT_URL` (required), `FARSIGHT_TOKEN` (required), `FARSIGHT_POLL` (default `15s`), `LOG_LEVEL` (`debug`/`info`, default `info`). Logs are JSON through slog.

- [ ] **Step 1: Write the failing tests** — `internal/agent/agent_test.go`

```go
package agent

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/save/savetest"
)

type sink struct {
	mu     sync.Mutex
	posts  []extract.Snapshot
	status int
}

func (s *sink) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/ingest/mv/snapshot" ||
			r.Header.Get("Authorization") != "Bearer sekrit" || r.Header.Get("Content-Encoding") != "gzip" {
			t.Errorf("bad request: %s %s %v", r.Method, r.URL.Path, r.Header)
		}
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		b, _ := io.ReadAll(zr)
		var snap extract.Snapshot
		if err := json.Unmarshal(b, &snap); err != nil {
			t.Error(err)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.posts = append(s.posts, snap)
		if s.status != 0 {
			w.WriteHeader(s.status)
		}
	}
}

func newAgent(t *testing.T, dir, url string) *Agent {
	return New(Config{WorldsDir: dir, WorldName: "W", ServerID: "mv", URL: url, Token: "sekrit"},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

var zdos = []savetest.ZDO{{Prefab: "portal_wood", Strings: map[string]string{"tag": "home"}}}

func TestChunkedPostsOnceImmediately(t *testing.T) {
	s := &sink{}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()
	dir := t.TempDir()
	savetest.WriteChunkedWorld(t, dir, "W", 5, "seed", zdos, nil, nil, nil)
	a := newAgent(t, dir, srv.URL)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := a.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.posts) != 1 || s.posts[0].SaveID != "chunked:5" || s.posts[0].ServerID != "mv" || len(s.posts[0].Markers) != 1 {
		t.Fatalf("posts = %+v", s.posts)
	}
	savetest.WriteChunkedWorld(t, dir, "W", 6, "seed", zdos, nil, nil, nil)
	if err := a.Tick(ctx); err != nil || len(s.posts) != 2 || s.posts[1].SaveID != "chunked:6" {
		t.Fatalf("second save: err=%v posts=%d", err, len(s.posts))
	}
}

func TestLegacyWaitsForStableSave(t *testing.T) {
	s := &sink{}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()
	dir := t.TempDir()
	savetest.WriteLegacyWorld(t, dir, "W", "seed", zdos, nil, nil, nil)
	a := newAgent(t, dir, srv.URL)
	ctx := context.Background()
	if err := a.Tick(ctx); err != nil || len(s.posts) != 0 {
		t.Fatalf("first tick must only record a candidate: err=%v posts=%d", err, len(s.posts))
	}
	if err := a.Tick(ctx); err != nil || len(s.posts) != 1 {
		t.Fatalf("second tick must post: err=%v posts=%d", err, len(s.posts))
	}
}

func TestFailedPostIsRetried(t *testing.T) {
	s := &sink{status: http.StatusInternalServerError}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()
	dir := t.TempDir()
	savetest.WriteChunkedWorld(t, dir, "W", 1, "seed", zdos, nil, nil, nil)
	a := newAgent(t, dir, srv.URL)
	ctx := context.Background()
	if err := a.Tick(ctx); err == nil {
		t.Fatal("want error on 500")
	}
	s.status = 0
	if err := a.Tick(ctx); err != nil || len(s.posts) != 2 {
		t.Fatalf("retry: err=%v posts=%d", err, len(s.posts))
	}
}

func TestNoSaveIsQuiet(t *testing.T) {
	a := newAgent(t, t.TempDir(), "http://127.0.0.1:1")
	if err := a.Tick(context.Background()); err != nil {
		t.Fatalf("missing save must not error: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agent/`
Expected: FAIL with `undefined: New`.

- [ ] **Step 3: Implement** — `internal/agent/agent.go`

```go
// Package agent watches a Valheim world for new saves and pushes atlas
// snapshots to the central Farsight app.
package agent

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jumpingmushroom/farsight/internal/extract"
	"github.com/jumpingmushroom/farsight/internal/save"
)

type Config struct {
	WorldsDir string
	WorldName string
	ServerID  string
	URL       string
	Token     string
	Poll      time.Duration
}

type Agent struct {
	cfg       Config
	log       *slog.Logger
	client    *http.Client
	lastSent  string
	candidate string
}

func New(cfg Config, log *slog.Logger) *Agent {
	if cfg.Poll == 0 {
		cfg.Poll = 15 * time.Second
	}
	return &Agent{cfg: cfg, log: log, client: &http.Client{Timeout: 60 * time.Second}}
}

func (a *Agent) Tick(ctx context.Context) error {
	id, format, err := save.LatestSave(a.cfg.WorldsDir, a.cfg.WorldName)
	if errors.Is(err, save.ErrNoSave) || errors.Is(err, save.ErrSaveInProgress) {
		a.log.Debug("no complete save", "reason", err)
		return nil
	}
	if err != nil {
		return err
	}
	if id == a.lastSent {
		return nil
	}
	if format == save.FormatLegacy && id != a.candidate {
		a.candidate = id
		return nil
	}
	start := time.Now()
	e := extract.New()
	w, err := save.Read(a.cfg.WorldsDir, a.cfg.WorldName, e.Add)
	if errors.Is(err, save.ErrSaveChanged) || errors.Is(err, save.ErrSaveInProgress) {
		a.log.Info("save changed while reading; will retry", "save", id)
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", id, err)
	}
	snap := e.Finish(w, a.cfg.ServerID, time.Now().UTC())
	if err := a.post(ctx, snap); err != nil {
		return fmt.Errorf("post %s: %w", w.SaveID, err)
	}
	a.lastSent = w.SaveID
	a.log.Info("snapshot sent", "save", w.SaveID, "zdos", w.ZDOCount,
		"markers", len(snap.Markers), "unknownPrefabs", snap.Stats.UnknownPrefabs,
		"took", time.Since(start).Round(time.Millisecond))
	return nil
}

func (a *Agent) post(ctx context.Context, snap *extract.Snapshot) error {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(zw).Encode(snap); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	url := fmt.Sprintf("%s/ingest/%s/snapshot", a.cfg.URL, a.cfg.ServerID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("status %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	return nil
}

func (a *Agent) Run(ctx context.Context) error {
	t := time.NewTicker(a.cfg.Poll)
	defer t.Stop()
	for {
		if err := a.Tick(ctx); err != nil {
			a.log.Error("tick failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}
```

Note that the test `TestFailedPostIsRetried` counts two posts: the failed one, which the sink still records, and the retry. That is intended.

- [ ] **Step 4: Implement the binary** — `cmd/farsight-agent/main.go`

```go
// Command farsight-agent runs beside a Valheim server, reads each new world
// save and pushes an atlas snapshot to Farsight.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jumpingmushroom/farsight/internal/agent"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	level := slog.LevelInfo
	if env("LOG_LEVEL", "info") == "debug" {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	poll, err := time.ParseDuration(env("FARSIGHT_POLL", "15s"))
	if err != nil {
		log.Error("bad FARSIGHT_POLL", "err", err)
		os.Exit(2)
	}
	cfg := agent.Config{
		WorldsDir: env("FARSIGHT_WORLDS_DIR", "/worlds/worlds_local"),
		WorldName: os.Getenv("WORLD_NAME"),
		ServerID:  os.Getenv("FARSIGHT_SERVER_ID"),
		URL:       os.Getenv("FARSIGHT_URL"),
		Token:     os.Getenv("FARSIGHT_TOKEN"),
		Poll:      poll,
	}
	for k, v := range map[string]string{"WORLD_NAME": cfg.WorldName, "FARSIGHT_SERVER_ID": cfg.ServerID, "FARSIGHT_URL": cfg.URL, "FARSIGHT_TOKEN": cfg.Token} {
		if v == "" {
			log.Error("missing required env", "var", k)
			os.Exit(2)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	log.Info("farsight-agent starting", "world", cfg.WorldName, "server", cfg.ServerID, "poll", cfg.Poll)
	if err := agent.New(cfg, log).Run(ctx); err != nil && ctx.Err() == nil {
		log.Error("agent stopped", "err", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 5: Run the whole suite**

Run: `gofmt -l . ; go vet ./... && go test ./... && go build ./cmd/...`
Expected: all packages `ok`, and the build succeeds.

- [ ] **Step 6: Smoke-test against a golden world with a throwaway listener**

```bash
go build -o /tmp/fa ./cmd/farsight-agent
(python3 -m http.server 18080 >/dev/null 2>&1 &) ; sleep 1
FARSIGHT_WORLDS_DIR=testdata-golden/chunked WORLD_NAME=MuleVikings FARSIGHT_SERVER_ID=mv \
FARSIGHT_URL=http://127.0.0.1:18080 FARSIGHT_TOKEN=x FARSIGHT_POLL=2s timeout 5 /tmp/fa || true
pkill -f 'http.server 18080'
```
Expected: JSON log lines `farsight-agent starting`, then `tick failed` with `status 501` (python's server refuses POST), repeating every 2 s. That proves the read, extract and post path runs end to end against a real save.

- [ ] **Step 7: Commit**

```bash
git add internal/agent cmd/farsight-agent
git commit -m "feat(agent): save watcher that pushes gzip snapshots to Farsight"
```

---

## After this plan

- Plan 2 (world generation and tiles) consumes `extract.WorldInfo.Seed` and `GenVersion`. Its biome-oracle test reuses `testdata-golden/chunked` through `save.Read`.
- Plan 3 adds the log tailer to `internal/agent` and a second POST endpoint, `/ingest/{server}/events`.
- Plan 4 implements the receiving end of this plan's snapshot contract.
- Plan 5 builds the images and wires the sidecar into `base-valheim`, using the timings recorded in Task 8.
