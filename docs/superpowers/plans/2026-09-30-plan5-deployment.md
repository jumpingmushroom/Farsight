# Farsight Plan 5 — Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Farsight deployable. That means container images for `farsight` (with the web UI) and `farsight-agent`, a GitHub Actions CI that tests and publishes them to ghcr, and the cloudcluster GitOps manifests for the app and the Valheim sidecars. Everything is prepared locally, and nothing is pushed.

**Architecture:** Two small Go-only runtime images are built by multi-stage Dockerfiles in CI; there is no Docker here.
- **farsight** runs as a non-root Deployment in its own namespace, with a PVC, ingress and Cilium policies.
- **farsight-agent** runs as a sidecar in every Valheim pod through `base-valheim`.
- **Agent traffic** goes over a separate in-cluster ingest port that the public ingress never routes.

Cloudcluster changes are made on a local branch `feat/farsight` in `/workspace/cloudcluster`. Nothing is pushed until the user gives the go-ahead (Task 8).

**Tech Stack:** Go 1.27, SvelteKit (Node 24), Docker multi-stage and distroless, GitHub Actions, ghcr.io, Kubernetes with kustomize and Flux v2, Cilium, ingress-nginx, cert-manager, sops/age.

**Spec:** `docs/superpowers/specs/2026-09-29-farsight-atlas-mvp-design.md` (Delivery step 7, farsight-agent and central sections). **Cluster conventions:** `/workspace/cloudcluster/CLAUDE.md`, `docs/runbooks/deploying-an-app.md`, `docs/runbooks/gameservers.md`. They are binding for every cloudcluster file.

## Global Constraints

- **Never push, to either repo.** Never `kubectl apply`, `patch`, `exec` or `flux reconcile` against the cluster before Task 8. Read-only `kubectl get` is allowed with `KUBECONFIG=~/.kube/cloudcluster.yaml`.
- The Farsight repo is `github.com/jumpingmushroom/Farsight` (renamed with a capital F on 2026-09-30). The Go module path stays `github.com/jumpingmushroom/farsight`: GitHub resolves repo names case-insensitively. Image names stay lowercase, because ghcr requires it, so the workflow must spell them out and not derive them from `github.repository`. It is **public** (changed at the user's request on 2026-09-30; it is still empty). Images are the **public** packages `ghcr.io/jumpingmushroom/farsight` and `ghcr.io/jumpingmushroom/farsight-agent`, so no image pull secrets are used anywhere.
- **Public repo hygiene:** nothing sensitive goes into the Farsight repo. That means no secrets, no real player names or IDs, no internal hostnames, IPs or tailnet names, no decompiled code, and no save or log data. Cluster-specific values (server addresses, hashes, secrets) live only in cloudcluster.
- **Image builds:**
  - `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1`, `-trimpath -ldflags "-s -w"`.
  - `farsight` is built with `-tags webui` after `cd web && npm ci && npm run build`.
  - `TestTerrainDigest` (`go test ./internal/worldgen -run TestTerrainDigest`) must run inside the farsight image build with `GOAMD64=v1`, so a floating-point drift fails the build.
- **Runtime images:** `gcr.io/distroless/static-debian12:nonroot` for farsight (uid 65532). The agent image uses the same base, but the pod overrides it to run as uid 0 (see below). Both binaries import `time/tzdata`, so no tzdata is needed in the image.
- **Cluster image refs:** pinned as `ghcr.io/jumpingmushroom/<name>:<short-sha>@sha256:<digest>`. Never `:latest@sha256`. The runbook requires this.
- **Pods:**
  - `enableServiceLinks: false` on every pod. A Service named `farsight` would otherwise inject `FARSIGHT_*` env vars.
  - `automountServiceAccountToken: false`.
  - `seccompProfile: RuntimeDefault` where the pod allows it.
  - Every container: `allowPrivilegeEscalation: false`, `capabilities.drop: ["ALL"]`, `readOnlyRootFilesystem: true`.
- **farsight pod:** `runAsNonRoot: true`, `runAsUser/runAsGroup/fsGroup: 65532`. Resources: requests `cpu 50m, memory 128Mi`, limit `memory 1Gi`, no CPU limit. Env `GOMEMLIMIT=800MiB`.
- **Agent sidecar:**
  - It is a copy of the mtail posture: `runAsUser: 0`, caps dropped, read-only rootfs. supervisord writes its logs as root-owned 0600.
  - Resources: requests `cpu 50m, memory 64Mi`, limits `cpu 1, memory 768Mi`. Env `GOMEMLIMIT=600MiB`.
  - Mounts: `data` subPath `config` at `/worlds` (read-only), and `supervisor-logs` at `/var/log/supervisor` (read-only).
- **Timezone:** `FARSIGHT_LOG_TZ=Europe/Oslo` only on the mulevikings instance. mulevikings-old and muleadventure keep the default `UTC`.
- **Ingest port:** the agent → farsight traffic uses the new in-cluster ingest port 8081 (Task 1). The public Ingress targets only port 8080, which never serves `/ingest/`.
- **Secrets:**
  - Secrets live only in sops files encrypted with `--encrypted-regex '^(data|stringData)$'`, and only to the age recipient in `/workspace/cloudcluster/.sops.yaml`. Never commit plaintext.
  - Generated plaintext values go to `~/.config/farsight/secrets.txt` (mode 0600) for the user. They are never printed in chat, logs, commits or reports.
  - The ConfigMap holds only bcrypt hashes from `farsight hash`.
- **Commits:** Farsight repo commits use `-c user.name="Johnny" -c user.email="johnny@jumpingmushroom.com"`. Cloudcluster commits use `-c user.name="Johnny Dalen" -c user.email="johnny@jumpingmushroom.com"` in Conventional Commits with a scope (`feat(farsight): …`, `feat(gameservers): …`), on the local branch `feat/farsight`. Every commit message ends with a blank line, then `Claude-Session: https://claude.ai/code/session_01NZ5db8DZiuc7ZwQY5wLn7T`.
- **Never commit** `reference/`, `testdata-golden/`, `web/build`, `node_modules` or plaintext secrets.

---

### Task 1: Separate in-cluster ingest listener, gzip JSON, tzdata

The ingest endpoints must not be reachable through the public ingress. That was a Plan 4a carry-over.

**Files:**
- Modify: `internal/config/config.go` and `config_test.go`, adding `IngestListen string \`json:"ingestListen"\`` (default `""`).
- Modify: `internal/server/server.go` and `server_test.go`.
- Modify: `cmd/farsight/serve.go` and `serve_test.go`.
- Modify: `cmd/farsight/main.go` and `cmd/farsight-agent/main.go`, each adding `import _ "time/tzdata"`.

**Interfaces:**
- Produces:
  - `server.New(d Deps) http.Handler`, unchanged: the public handler.
  - New `server.NewIngest(d Deps) http.Handler`: only `POST /ingest/{server}/snapshot`, `POST /ingest/{server}/events` and `GET /healthz`, with everything else returning a JSON 404.
  - `Deps` gains `SplitIngest bool`. When true, the public handler returns the JSON 404 for all `/ingest/` paths.
- **Config:**
  - `ingestListen: ":8081"` enables the split.
  - Empty keeps today's single-listener behaviour, and the e2e tests keep working.
  - The config test also covers the validation: `ingestListen` must differ from `listen`.
- **serve:** when `IngestListen` is set, run a second `http.Server` with the same timeouts on it, and shut both down gracefully.
- **Gzip:** add response compression for `GET /api/servers/{id}` and `GET /api/servers/{id}/snapshot` when `Accept-Encoding` contains `gzip`.
  - Wrap with a small `gzipJSON` middleware using `compress/gzip`, a sync.Pool of writers, and `Vary: Accept-Encoding`. Don't compress tiles (PNG) or responses under 1 KiB.
  - The snapshot is the largest payload and is refetched on every save (a Plan 4b carry-over).

- [ ] **Step 1: Failing tests.**
  - `server_test.go`: with `SplitIngest: true`, a POST to `/ingest/demo/events` on the public handler returns 404, and the same request on `NewIngest` returns 200 (reuse the existing ingest test helpers).
  - On `NewIngest`, `/api/servers` returns 404 and `/healthz` returns 200.
  - Gzip: a card request with `Accept-Encoding: gzip` gets `Content-Encoding: gzip` and a body that gunzips to the same JSON. Without the header, the body is plain. A tile never gets gzip.
  - `config_test.go`: `ingestListen` defaults to empty, and an `ingestListen` equal to `listen` is rejected.
  - `serve_test.go`: with `ingestListen` set to `127.0.0.1:0` as well, `runServe` serves `/healthz` on both, and ingest works only on the ingest port.
- [ ] **Step 2: Implement.** Then run `go test ./... -count=1`, `gofmt -l .`, `go vet ./...` and `go vet -tags webui ./...` (after `make web`), and run the e2e suite once (it uses the single-listener config).
- [ ] **Step 3: Commit** `feat(server): in-cluster ingest listener, gzip JSON, embedded tzdata`.

---

### Task 2: Web carry-overs: fetch timeout, fog test gaps

**Files:**
- Modify: `web/src/lib/api.ts` and `api.test.ts`.
- Modify: `web/src/lib/fog.ts` (only if needed) and `fog.test.ts`.
- Modify: `web/tests/e2e/helpers.ts` (the guard self-tests).

- [ ] **Step 1: Failing tests, then implement.**
  - **Timeout.** Every `api.ts` request gets a timeout: 20 s for servers and card, 60 s for the snapshot, 15 s for unlock.
    - Use `AbortController` plus `setTimeout`. Don't use `AbortSignal.timeout`, because Safari before 16 lacks it.
    - A timeout rejects with `ApiError(0, 'timeout')`, so the single-flight poll in `state.svelte.ts` ends and the next tick retries.
    - Test it with a fake fetch that never resolves and fake timers.
  - **Fog test gaps.** Add a `boxBlur` test with content on the image border, checked against a brute-force reference. Add a `releaseScratch` test: after `_destroyContainer` the scratch canvas has width and height 0, and a later draw recreates it.
  - **Guard self-tests.** They must assert the specific failure, not a bare `test.fail()`. For example, catch the guard's error or check its message.
- [ ] **Step 2:** Run `npm run check`, `npm test` and `npm run build`, then run the e2e suite once.
- [ ] **Step 3: Commit** `fix(web): request timeouts; fog and guard test gaps`.

---

### Task 2b: Public-readiness scrub (added after the history audit)

A read-only audit found real personal data at HEAD and throughout the history. None of it is a secret. The finding IDs below come from the audit report, which is in the SDD workspace as `audit-report.md`.

- **B1–B2:** live log excerpts in `internal/logwatch/testdata/*.log`. They contain real player names, Steam64, PlayStation and PlayFab IDs, join codes and the server IP.
- **B3–B4:** real save slices in `internal/save/testdata/chunked/{main.fwl2,portals.chunk}`, containing Steam64 IDs, player names as portal tags, and coordinates.
- **S1:** real `main.chunks` and `main.db2`.
- **B6–B10:** real names and IDs in the tests `logwatch/parse_test.go`, `session_test.go`, `save/zdo_test.go`, `extract/extract_test.go`, `bases_test.go` and `save/legacy_test.go`.
- **B11–B12:** the same data in the plan 1 and plan 3 docs.
- **S2:** real world seeds in tests and docs.
- **B5:** a commit message listing six player names with their session seconds.

**Approach:** scrub the current tree, then publish one fresh root commit (Task 8). The full private history stays on a local-only tag and is never pushed.

1. **Binary fixtures: same-length substitution.**
   - In every committed save fixture (`.fwl2`, `.chunk`, `.chunks`, `.db2`), replace each real player name, portal tag and sign text, and the world and seed names, with an invented string of exactly the same byte length.
   - Replace every real Steam64 / PlayFab / platform ID with fake values of the same width.
   - Replace the seed in `.fwl2` with a fake one.
   - The format is length-prefixed, so the files stay valid real-format data.
   - Write the substitution as a small, reviewable, one-off Go or Python script under the session scratchpad. It reads the real values from `testdata-golden` at run time; never commit the script or the values.
   - Then update each test's expectations: names, counts and seed-derived values.
2. **Text fixtures.** Rewrite the two logs with the same structure and timing, using invented names (Astrid, Bjorn, Sigrun…), fake IDs of the same shape, fake join codes, and a documentation IP (`203.0.113.x`). Update `parse_test.go` and `session_test.go` to match.
3. **Seeds.** Replace the real seeds used in tests and docs (e.g. the legacy world seed and any seed matching a golden world) with invented seed names. Regenerate the expected digests in `TestTerrainDigest` and in any other seed-derived expectations, using the current code. Leave terrain-oracle tests that read `testdata-golden` untouched; they skip without it.
4. **Docs.** In the plan 1 and plan 3 docs, the spec, and any other doc, replace real names, IDs, IPs other than the join address, and seeds with the same invented values. The server/community names Mulevikings, Mulennials and Muleadventure stay, as does `names.txt`. Both are owner decisions.
5. **`.gitignore`.** Add `.claude/`, `.superpowers/`, `/farsight-render` and `/farsight-seed`.
6. **Verify.**
   - Re-run the audit's reference extraction and search the tree for every real name, ID, IP and seed. Expect 0 hits outside `testdata-golden/`, which is ignored.
   - Run `go test ./... -count=1` (green both with and without `testdata-golden` present, i.e. in a clean worktree) and the full e2e suite.
7. **Commit** `test: replace live-server fixtures and seeds with synthetic data`. The history rewrite itself happens in Task 8.

---

### Task 3: Dockerfiles

**Files:** Create `Dockerfile` (the farsight image), `Dockerfile.agent` and `.dockerignore`.

`Dockerfile`:

```dockerfile
# syntax=docker/dockerfile:1.7
FROM node:24-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-bookworm AS build
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/build ./web/build
# Bit-exact terrain on the target microarchitecture level, or the build fails.
RUN go test ./internal/worldgen -run TestTerrainDigest -count=1
RUN go build -tags webui -trimpath -ldflags "-s -w" -o /out/farsight ./cmd/farsight

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/farsight /usr/local/bin/farsight
USER 65532:65532
EXPOSE 8080 8081
ENTRYPOINT ["/usr/local/bin/farsight"]
CMD ["serve", "-config", "/etc/farsight/farsight.json"]
```

`Dockerfile.agent`:

```dockerfile
# syntax=docker/dockerfile:1.7
FROM golang:1.27-bookworm AS build
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags "-s -w" -o /out/farsight-agent ./cmd/farsight-agent

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/farsight-agent /usr/local/bin/farsight-agent
ENTRYPOINT ["/usr/local/bin/farsight-agent"]
```

`.dockerignore`: `.git`, `reference/`, `testdata-golden/`, `web/node_modules`, `web/build`, `web/.svelte-kit`, `web/test-results`, `web/playwright-report`, `.superpowers/`, `/farsight`, `/farsight-*`, `design/`, `docs/`, `hack/`.

- [ ] **Step 1: Check the Go version.** Confirm that the `go.mod` `go` directive is satisfied by `golang:1.27-bookworm`. If `go.mod` says `1.27.1`, the image tag `1.27` resolves to the latest 1.27.x, which is fine. Confirm that `node:24-bookworm-slim` matches the `engines` field.
- [ ] **Step 2: Local check without Docker.** Emulate the build commands in a clean `git worktree` of HEAD, which has no `testdata-golden` and no `reference`:
  - run `cd web && npm ci && npm run build`;
  - run the three `go` commands with the same env;
  - confirm that `TestTerrainDigest` passes with `GOAMD64=v1`;
  - confirm that both binaries are static (`file` or `ldd` says "not a dynamic executable").
- [ ] **Step 3: Validate the Dockerfiles.** If `hadolint` can be downloaded as a static binary into `~/.local/bin`, lint both files. Otherwise note that it was skipped.
- [ ] **Step 4: Commit** `build: Dockerfiles for farsight and farsight-agent`.

---

### Task 4: GitHub Actions CI

**Files:** Create `.github/workflows/ci.yml` and `.github/workflows/images.yml`.

`ci.yml` runs on `push` to any branch and on `pull_request`. It has three jobs, and every action is pinned to a full commit SHA with a version comment, as the existing image workflows in cloudcluster do.
- **go:** `actions/setup-go` with `go-version-file: go.mod`, then:
  - `gofmt -l . | tee /dev/stderr | (! read)`;
  - `go vet ./...`;
  - `go test -race ./... -count=1`. CI has gcc, which was a Plan 1 carry-over.
- **web:** `actions/setup-node` with node 24 and npm cache on `web/package-lock.json`, then `npm ci`, `npm run check`, `npm test` and `npm run build`.
- **e2e:** needs go and web. It sets up both toolchains, runs `cd web && npm ci && npx playwright install --with-deps chromium-headless-shell`, then `make e2e` with `CI=true`. It uploads `web/playwright-report` and `web/test-results` as an artifact on failure only.

`images.yml` runs on `push` to `main` and on `workflow_dispatch`, with `permissions: { contents: read, packages: write }`. Its job builds a matrix over `{ name: farsight, file: Dockerfile }` and `{ name: farsight-agent, file: Dockerfile.agent }` using:
- `docker/setup-buildx-action`;
- `docker/login-action` for ghcr with `GITHUB_TOKEN`;
- `docker/metadata-action`, with tags `type=sha,prefix=,format=short` and `type=raw,value=latest`;
- `docker/build-push-action` with `platforms: linux/amd64`, `provenance: false`, `cache-from/to: type=gha,scope=${{ matrix.name }}`, then `push: true`.

It ends with a step that writes the pushed digest, `${{ steps.build.outputs.digest }}`, and the short sha to the job summary, so Task 8 can pin them.

- [ ] **Step 1: Verify the tests pass in a clean checkout.** In a clean `git worktree` of HEAD, which has no `testdata-golden`, run `go test ./... -count=1`. Every golden-data test must skip, not fail. If any fails, fix it so it calls `t.Skip` when its data is missing, and commit that fix separately first.
- [ ] **Step 2: Lint the workflows.** Download the `actionlint` static binary into `~/.local/bin` and run it on both workflows. Resolve the pinned SHAs with `gh api repos/<owner>/<action>/git/ref/tags/<tag>`, a read-only API call.
- [ ] **Step 3: Commit** `ci: tests, e2e and ghcr image builds`.

---

### Task 5: Local secret tooling and generated secrets

**Files:** Create `hack/gen-secrets.sh` and `~/.config/farsight/secrets.txt`. The secrets file is outside the repo and never committed.

- [ ] **Step 1: Install tooling.** Install `sops` (v3.10+) and `age` as static release binaries from their GitHub releases into `~/.local/bin`. Verify them against the published checksums.
- [ ] **Step 2: Write `hack/gen-secrets.sh`.** It is idempotent: if the secrets file exists, it reads it instead of regenerating. It generates:
  - `FARSIGHT_COOKIE_KEY`: 48 random bytes, base64url, from `head -c 48 /dev/urandom`.
  - One agent token per server (`mulevikings`, `mulevikings-old`, `muleadventure`): 32 random bytes, base64url.
  - One passphrase per server: four random words from a built-in list, joined with `-`, for example `amber-fjord-raven-oak`. Use a 256-word list embedded in the script; it should be easy to type on a phone.

  It writes them to `~/.config/farsight/secrets.txt` with mode 0600, including the share links `https://farsight.apps.jumpingmushroom.com/#s=<id>&k=<url-encoded passphrase>`. It then prints only the file path, never a value.
- [ ] **Step 3: Hashing helper.** Add `hack/hash-secrets.sh`. It pipes each passphrase and token through `go run ./cmd/farsight hash` and prints `id passphraseHash agentTokenHash` lines. Hashes aren't secret; they go into the ConfigMap.
- [ ] **Step 4: Commit** `build: secret generation and hashing helpers` (the scripts only).

---

### Task 6: cloudcluster: `apps/farsight`

Work in `/workspace/cloudcluster` on the local branch `feat/farsight`, created from `main`. Follow `docs/runbooks/deploying-an-app.md` and copy the shape of an existing app. Deviations are listed here.

**Files, in `clusters/prod/apps/farsight/`:**
- `namespace.yaml`: PSA `restricted` for enforce, audit and warn.
- `configmap.yaml`: ConfigMap `farsight-config` with key `farsight.json`:
  ```json
  {"listen":":8080","ingestListen":":8081","dataDir":"/data","trustProxy":true,
   "servers":[
    {"id":"mulevikings","name":"Mulevikings","crossplay":true,"address":"87.238.54.172:2456","maxPlayers":10,"passphraseHash":"…","agentTokenHash":"…"},
    {"id":"mulevikings-old","name":"Mulevikings Old","crossplay":false,"address":"<LB IP>:2456","maxPlayers":10,"passphraseHash":"…","agentTokenHash":"…"},
    {"id":"muleadventure","name":"Muleadventure","crossplay":false,"address":"<LB IP>:2456","maxPlayers":10,"passphraseHash":"…","agentTokenHash":"…"}]}
  ```
  - Take the names from each overlay's `SERVER_NAME`.
  - Take the addresses from the overlay Service or LoadBalancer annotations, or from read-only `kubectl get svc -n gameservers`. If an address can't be determined, omit the field.
  - Fill the hashes from Task 5's helper.
  - `discordHint` is omitted; the user can add it.
- `app-secret.sops.yaml`: Secret `farsight-app` with `stringData.FARSIGHT_COOKIE_KEY`, encrypted.
- `pvc.yaml`: `farsight-data`, `cinder-rlcs`, 5Gi.
- `deployment.yaml`: Deployment and Service `farsight`.
  - Deployment: `replicas: 1`, `strategy: Recreate`, `app: farsight`.
  - Pod: `enableServiceLinks: false`, `automountServiceAccountToken: false`, no `imagePullSecrets` (the images are public). securityContext `runAsNonRoot/runAsUser/runAsGroup/fsGroup 65532`, seccomp `RuntimeDefault`.
  - Container: image per Global Constraints, from `IMAGE_FARSIGHT`, which is set in Task 8. Until then use the placeholder `ghcr.io/jumpingmushroom/farsight:PENDING@sha256:000…` (64 zeros) and list it in Task 8.
    - args: `["serve","-config","/etc/farsight/farsight.json"]`.
    - env: `GOMEMLIMIT=800MiB`, `TZ=Europe/Oslo`, and `FARSIGHT_COOKIE_KEY` from `secretKeyRef farsight-app`.
    - ports: `http 8080`, `ingest 8081`.
    - Mounts: `data` at `/data`; `config` from `configMap farsight-config` at `/etc/farsight` (read-only); an emptyDir at `/tmp`.
    - readOnlyRootFilesystem.
    - Probes: httpGet `/healthz` on 8080. Liveness 15/30, readiness 5/10.
    - resources per Global Constraints.
  - Service: ports `http 8080` and `ingest 8081`.
- `ingress.yaml`: host `farsight.apps.jumpingmushroom.com`.
  - No SSO-gate annotations: the ingress is not SSO-gated, because Farsight has its own passphrase auth. Add a comment saying so.
  - `cert-manager.io/cluster-issuer: letsencrypt-prod`, TLS secret `farsight-tls`.
  - Backend: service `farsight` port 8080 only.
  - `nginx.ingress.kubernetes.io/limit-rps: "20"`.
- `networkpolicy.yaml`: CiliumNetworkPolicy `farsight`, selecting `app: farsight`.
  - Ingress from `ingress-nginx` on 8080.
  - Ingress from blackbox on 8080.
  - Ingress from `{io.kubernetes.pod.namespace: gameservers, app: valheim}` on 8081 only.
  - Egress to kube-dns only.
- `networkpolicy-namespace.yaml`: the default namespace policy plus the `farsight-acme-solver` rule on 8089, copied from an existing app. Drop the backup egress rule, since there are no backups yet (see below).
- `kustomization.yaml`: `namespace: farsight`, the resources, and the service-monitor component with a Probe patch targeting `http://farsight.farsight.svc.cluster.local:8080/healthz`.
- **Backups:** skipped for the MVP.
  - The data is rebuildable: snapshots are re-pushed by the agents and tiles are re-rendered. Only session history would be lost.
  - A backup CronJob needs a bucket and credentials on an off-cluster object store, which is outside this repo.
  - Record this in Task 7's runbook as a follow-up.

Also modify: `clusters/prod/apps/kustomization.yaml`, appending `- farsight`.

- [ ] **Step 1: Write the files.** Encrypt the secrets with the command from CLAUDE.md, adapted to this machine's absolute path: `sops --config /workspace/cloudcluster/.sops.yaml --encrypt --encrypted-regex '^(data|stringData)$' --in-place /workspace/cloudcluster/clusters/prod/apps/farsight/<file>`. Then run `grep -L 'ENC\[' *.sops.yaml` (must print nothing) and grep the whole app dir for any secret values from `secrets.txt` (must find none).
- [ ] **Step 2: Render.** `kubectl kustomize clusters/prod/apps/farsight` must render without errors, and so must `kubectl kustomize clusters/prod/apps`. Check the rendered output for:
  - `enableServiceLinks: false`;
  - no `:latest`;
  - that the ingress backend is port 8080;
  - that the Probe has `release: monitoring-kube-prometheus-stack`.
- [ ] **Step 3: Commit** on `feat/farsight`: `feat(farsight): namespace, deployment, ingress and policies`.

---

### Task 7: cloudcluster: agent sidecar in the Valheim pods, plus the runbook

**Files, in `clusters/prod/apps/gameservers/`:**
- `base-valheim/deployment.yaml`: add a container `farsight-agent` after mtail.
  - image per Global Constraints (`IMAGE_AGENT`). Until Task 8 use the placeholder `ghcr.io/jumpingmushroom/farsight-agent:PENDING@sha256:000…` (64 zeros).
  - securityContext: the same as mtail (`runAsUser: 0`, `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`, drop ALL).
  - env:
    - `WORLD_NAME` from the game container. Kustomize can't reference another container's env, so each overlay sets it.
    - `FARSIGHT_SERVER_ID`: overlay.
    - `FARSIGHT_URL=http://farsight.farsight.svc.cluster.local:8081`.
    - `FARSIGHT_TOKEN` from `secretKeyRef: { name: farsight-agent, key: FARSIGHT_TOKEN }`.
    - `FARSIGHT_WORLDS_DIR=/worlds/worlds_local`.
    - `FARSIGHT_LOG_DIR=/var/log/supervisor`.
    - `GOMEMLIMIT=600MiB`.
  - volumeMounts:
    - `{name: data, mountPath: /worlds, subPath: config, readOnly: true}`
    - `{name: supervisor-logs, mountPath: /var/log/supervisor, readOnly: true}`
  - resources per Global Constraints.
- `base-valheim/kustomization.yaml`: no change unless needed.
- `farsight-agent-networkpolicy.yaml`: a new top-level file listed in `gameservers/kustomization.yaml`.
  - CiliumNetworkPolicy `farsight-agent-egress`, selecting `app: valheim`.
  - Egress to `{io.kubernetes.pod.namespace: farsight, app: farsight}` on 8081/TCP.
  - It's a separate policy because policies are additive, so this avoids touching mulevikings' merge-replaced egress list. Add a comment explaining why.
- Each overlay (`instances/mulevikings`, `mulevikings-old`, `muleadventure`):
  - `patch.yaml` adds a `farsight-agent` container entry with env `WORLD_NAME` (the same value as the game container's), `FARSIGHT_SERVER_ID` (the instance id), and for mulevikings only `FARSIGHT_LOG_TZ=Europe/Oslo`.
  - Do not mount mulevikings' `zoneinfo` volume into the agent: it embeds tzdata.
  - `farsight-agent.sops.yaml`: Secret `farsight-agent` with `stringData.FARSIGHT_TOKEN` (encrypted), added to the overlay's `resources`. `namePrefix` renames it per instance, as with `password.sops.yaml`.
- **Enshrouded instances:** check that the base change only affects `base-valheim` users. Enshrouded uses its own base; confirm it and leave it untouched.
- `docs/runbooks/farsight.md`, a new runbook covering:
  - what Farsight is;
  - where the secrets live, and that the plaintext is in `~/.config/farsight/secrets.txt` on the workstation;
  - how to rotate a passphrase (re-hash, update the ConfigMap, push);
  - how to add a server;
  - how to bump image digests;
  - the backup follow-up;
  - an ops note: changing a passphrase doesn't revoke existing unlock cookies; rotate `FARSIGHT_COOKIE_KEY` to revoke all;
  - that ingest is in-cluster only (port 8081).

- [ ] **Step 1: Write the files.** Encrypt them and verify as in Task 6.
- [ ] **Step 2: Render and check the output.** Run `kubectl kustomize clusters/prod/apps/gameservers`. In the output, all 3 Valheim Deployments have a `farsight-agent` container with the right `WORLD_NAME`, `FARSIGHT_SERVER_ID`, per-instance secret name (e.g. `mulevikings-farsight-agent`) and `FARSIGHT_LOG_TZ` (only mulevikings). The Enshrouded Deployments are unchanged: diff their rendered output against `main`, rendered in a temp worktree. The mulevikings CiliumNetworkPolicy is unchanged.
- [ ] **Step 3: Commit** on `feat/farsight`: `feat(gameservers): farsight-agent sidecar on the Valheim servers` and `docs(farsight): runbook`.

---

### Task 8: Go-live (GATED: only after the user's explicit go-ahead in chat)

Do not start this task without a new, explicit user message approving it. It pushes both repos, and the cloudcluster push restarts all three Valheim servers.

- [ ] **Step 0: Sensitive-data audit gate.** Re-run the history audit (secrets, real player names and IDs, infrastructure details, decompiled content) over `git log --all -p`. Any BLOCKER stops the push until the history is fixed. The user must also have confirmed they accept publishing the ported Iron Gate worldgen and save-format code.
- [ ] **Step 0b: Fresh public root.** Tag the private history locally with `git tag archive/private-history master`, and never push it. Create `public-main` as an orphan branch whose single commit has the scrubbed tree (message `Initial public release`, same author and trailer). Push only that branch as `main`. Local work continues on `public-main`.
- [ ] **Step 1: Push the Farsight repo.**
  - Run `git remote add origin https://github.com/jumpingmushroom/Farsight.git && git push -u origin public-main:main`, so the default branch is `main`, which the workflows trigger on.
  - Wait for `ci.yml` and `images.yml` to go green: `gh run watch`.
  - Read the short sha and digests from the images job summary or with `gh api`.
- [ ] **Step 2: Public images.** Confirm both ghcr packages are public, with an anonymous pull of the manifest: `curl -s https://ghcr.io/token?scope=repository:jumpingmushroom/farsight:pull` followed by a manifest HEAD. If they aren't, ask the user to set the package visibility to public in the GitHub UI. The API cannot change it.
- [ ] **Step 3: Pin the images.** Find the placeholder refs with `grep -rn 'PENDING@sha256' clusters/prod/apps` (two: the farsight Deployment and `base-valheim`). Replace each `:PENDING@sha256:000…` with `:<short-sha>@sha256:<digest>`, then render again and confirm the grep finds nothing.
- [ ] **Step 4: Push cloudcluster.**
  - Merge `feat/farsight` into `main` and push.
  - Run `flux reconcile source git flux-system && flux reconcile kustomization apps`.
  - Watch `kubectl -n farsight get pods,certificate,ingress` and `kubectl -n gameservers get pods`.
- [ ] **Step 5: Verify.**
  - The farsight pod is Ready, and the certificate is issued.
  - Each Valheim pod has 4 of 4 containers running, and the agent logs show a snapshot and events posted.
  - Unlock with a share link. The card shows live players and the map charts, then renders.
  - `curl -s -o /dev/null -w '%{http_code}' https://farsight.apps.jumpingmushroom.com/ingest/mulevikings/events -X POST` returns 404.
- [ ] **Step 6: Update the spec** "Delivery" section with what was deployed. Commit and push the Farsight repo.
