# Native localmode runtime distribution contract

**Status:** Draft — first checkpoint (plan step 1) of the "dependency-free
localmode" initiative. This document defines an interface and a set of
decisions; it ships **no runtime code**. The existing Docker Compose localmode
path is unchanged and remains the default engine.

**Audience:** engineers working on the multi-phase, cross-repo effort to let
`volcano start` run localmode with no Docker and no other external
dependencies — split across `volcano-cli` (this repo) and `volcano-hosting`.

## 1. Why this document exists first

The goal — a native localmode that downloads, verifies, installs, and
supervises a self-contained runtime pack — is a 12-phase effort that spans two
repos. `volcano-hosting` must produce local-tagged, migration-embedded,
signed, durable artifacts (steps 2–4); `volcano-cli` must consume them with a
runtime manager and process supervisor (steps 5–9). Those two streams can only
proceed in parallel if the **boundary between them is fixed first**.

So the first deliverable is not code. It is a ratified contract: the artifact
naming and layout, the manifest schema, the verification scheme, the bill of
materials each pack must contain, the install layout on disk, the trust model,
and the rollout sequence. Everything downstream references this document
instead of re-litigating the boundary in every PR.

Two things in here are **merge-blocking** for this document because they are
factual/sequencing corrections rather than open preferences (see §12):

1. The bill of materials must be complete and correct (§7) — including Redis,
   Mailpit, and the frontends HTTP ingress.
2. The DynamoDB Local → PostgreSQL log-store relocation must be sequenced as a
   `volcano-hosting`-owned prerequisite that gates CLI steps 5–9 (§8).

Everything else is expected to iterate in review.

## 2. Scope of this contract

In scope:

- The cross-repo **artifact interface** (§6): names, URL scheme, manifest, verification.
- The on-disk **install layout** the CLI owns (§5).
- The **bill of materials** every native pack must reproduce (§7), derived from
  the current Compose stack.
- The **data-service strategy**, including the DynamoDB Local replacement (§8).
- The **language-runtime packaging** policy (§9).
- The **trust / security model** for host execution (§10).
- **Retention / GC / uninstall** for the on-disk cache (§11).
- **Version compatibility**, **parity requirements**, and the **`--engine`
  rollout** sequence (§13–§15).
- A **phased roadmap** with owners (§16).

Out of scope (deliberately deferred to later phases and their own PRs):

- Any runtime-manager, downloader, supervisor, or bundling **code**.
- Any `volcano-hosting` workflow / publishing changes.
- The `--engine` flag itself (it lands with the step-5 runtime manager, not now).
- Changing or removing the current Docker localmode path.

## 3. Enforcement-ownership matrix

A CLI-authored contract cannot unilaterally decide things the CLI neither
builds nor verifies. Every assertion below is tagged with the repo/team that
**owns** it. Anything the CLI cannot enforce is a **proposal requiring the
owning repo's sign-off**, not a lock.

| Area | Assertion | Owner | Status |
|---|---|---|---|
| Install layout on disk | `~/.volcano/runtimes/localmode/<version>/<os>-<arch>/` (§5) | volcano-cli | **Locked** (CLI-owned) |
| Cache GC / retention / uninstall | CLI prunes per policy in §11 | volcano-cli | **Locked** (CLI-owned) |
| Artifact names + URL scheme | `volcano-local-runtime-<os>-<arch>.<ext>` at a durable release host (§6) | volcano-hosting CI | **Proposed** — needs hosting sign-off |
| Manifest schema | `manifest.json` shape in §6 | joint (cli consumes, hosting emits) | **Proposed** — needs hosting sign-off |
| Verification scheme | cosign signature **and** SHA-256 checksum manifest (§6) | volcano-hosting CI + cli | **Proposed** — needs hosting sign-off (key distribution TBD) |
| Signing / durability of artifacts | signed, immutable, non-ephemeral release assets | volcano-hosting CI | **Proposed** — needs hosting sign-off |
| Base-pack contents (BOM) | server + data services + embedded migrations (§7) | volcano-hosting (builds) | **Proposed** — needs hosting sign-off |
| DynamoDB → PostgreSQL log-store | server-side `local`-tag adapter, no Java (§8) | volcano-hosting (server binary) | **Proposed / direction agreed** — hosting implements & signs off |
| local-tagged / ForceLocalMode build | native binaries built `-tags local` with `ForceLocalMode=true` | volcano-hosting | **Proposed** — needs hosting sign-off |
| Migration embedding | migrations embedded in / shipped with the server binary | volcano-hosting | **Proposed** — needs hosting sign-off |
| Language-runtime packaging | on-demand, independently versioned packs (§9) | volcano-cli (fetch) + volcano-hosting (produce) | **Proposed** |
| Trust / sandbox model | v1 = disclosure + consent, no enforced isolation (§10) | joint | **Proposed / direction agreed** |
| `--engine` rollout | default `docker`; native opt-in until per-platform parity (§15) | volcano-cli | **Locked** (CLI-owned) |

> Reviewers from `volcano-hosting` should treat every "Proposed" row as a
> request for concrete accept/reject, not as a decision already made on their
> behalf.

## 4. Supported platforms

The native pack matrix mirrors the platforms the CLI already ships on and the
binary matrix `volcano-hosting` already builds:

| OS | Arch | pack `<os>-<arch>` | archive format |
|---|---|---|---|
| Linux | x86_64 | `linux-amd64` | `.tar.gz` |
| Linux | arm64 | `linux-arm64` | `.tar.gz` |
| macOS | x86_64 | `macos-amd64` | `.tar.gz` |
| macOS | arm64 | `macos-arm64` | `.tar.gz` |
| Windows | x86_64 | `windows-amd64` | `.zip` |

`<os>` normalizes Go's `GOOS` (`darwin` → `macos`); `<arch>` normalizes
`GOARCH` (`x86_64` → `amd64`). The CLI resolves its own `<os>-<arch>` at
runtime and fetches the matching pack.

## 5. Canonical install layout (CLI-owned)

```
~/.volcano/
└── runtimes/
    └── localmode/
        └── <version>/                # e.g. 0.6.0 — the runtime-pack version
            └── <os>-<arch>/          # e.g. macos-arm64
                ├── bin/               # native volcano-hosting server + service binaries
                ├── migrations/        # only if not embedded in the server binary
                ├── manifest.json      # version, commit, platform, contents, checksums
                └── .install.ok        # written last, atomically, marks a verified install
        └── language-packs/            # phase 8, on-demand, independently versioned
            └── <lang>/<version>/<os>-<arch>/
```

Runtime **state** (data dirs, logs, PIDs) is deliberately **not** under
`runtimes/` — it lives beside it and survives runtime upgrades:

```
~/.volcano/
├── runtimes/      # immutable, verified, replaceable packs (above)
└── localmode/     # mutable per-install state, upgrade-independent
    ├── data/      # postgres cluster, redis dump, log-store db, storage, functions
    ├── logs/      # captured per-process logs
    └── run/       # pid files, assigned-port records, supervisor lock
```

### Why this layout

- **`runtimes/localmode/<version>/<os>-<arch>/`** (the user-spec form) is
  chosen over the shorter `~/.volcano/localmode/<version>/` from the plan body.
  The per-`<os>-<arch>` leaf lets one cache hold multiple arches without
  collision (relevant on shared homes / network mounts), and the
  `runtimes/<kind>/` prefix lets language packs and future runtime kinds slot
  in as siblings without a later relayout. **The shorter
  `~/.volcano/localmode/<version>/` form is deprecated.**
- Separating immutable `runtimes/` from mutable `localmode/` state means an
  atomic pack upgrade never risks the user's data, and a corrupt pack can be
  deleted and re-downloaded without losing databases.
- `~/.volcano/runtimes/` is green-field today; nothing else writes there.

Atomic install rule: download to a temp path, verify (§6), extract into a
temp sibling, then rename into place and write `.install.ok` **last**. The CLI
must never execute a pack directory that lacks a valid `.install.ok`.

## 6. Cross-repo artifact interface (central deliverable)

This is what actually unblocks the other phases. `volcano-hosting` produces it;
`volcano-cli` consumes it.

### 6.1 Artifact names

```
volcano-local-runtime-linux-amd64.tar.gz
volcano-local-runtime-linux-arm64.tar.gz
volcano-local-runtime-macos-amd64.tar.gz
volcano-local-runtime-macos-arm64.tar.gz
volcano-local-runtime-windows-amd64.zip
```

Alongside each release:

```
checksums.txt                 # SHA-256 for every artifact + manifest
volcano-local-runtime-*.sig   # cosign signature per artifact  (scheme TBD, see 6.4)
volcano-local-runtime-*.sbom  # SBOM per artifact
```

### 6.2 URL scheme (proposed)

Durable, immutable, versioned release assets — **not** 10-day GitHub Actions
artifacts. Proposed shape (final host is hosting-owned):

```
https://<durable-release-host>/volcano-local-runtime/<channel>/<version>/<artifact>
```

- `<channel>`: `nightly` (rolling) and immutable `release` (`local-X.Y.Z`).
- The CLI pins an exact `<version>` from its compatibility map (§13); it never
  silently follows a moving tag for a pinned install.

### 6.3 `manifest.json` schema (proposed)

Every pack contains a manifest; the release also publishes it standalone so the
CLI can resolve/verify **before** downloading the full archive.

```json
{
  "schema_version": 1,
  "runtime": "localmode",
  "version": "0.6.0",
  "commit": "<git-sha of volcano-hosting build>",
  "built_at": "2026-08-01T00:00:00Z",
  "platform": { "os": "macos", "arch": "arm64" },
  "min_cli_version": "0.6.0",
  "max_cli_version": null,
  "contents": [
    { "name": "server",     "path": "bin/volcano-hosting", "version": "<sha>",     "sha256": "..." },
    { "name": "postgres",   "path": "bin/postgres",        "version": "16.x",      "sha256": "..." },
    { "name": "redis",      "path": "bin/redis-server",    "version": "7.x",       "sha256": "..." },
    { "name": "mailpit",    "path": "bin/mailpit",         "version": "1.30",      "sha256": "..." },
    { "name": "migrations", "path": "migrations",          "embedded": true }
  ],
  "archive_sha256": "..."
}
```

### 6.4 Verification (proposed default — needs hosting sign-off)

The CLI verifies, in order, before any extraction or execution:

1. **SHA-256** of the downloaded archive against `checksums.txt` /
   `archive_sha256`.
2. **cosign signature** of the archive (keyless/Fulcio or a hosting-managed
   key — key distribution is the open question below).

Both must pass. Key distribution / trust root is **not decided here** and is
flagged for `volcano-hosting` sign-off. Presented as a concrete default so
hosting has something to accept or reject rather than an open-ended question.

## 7. Bill of materials (derived from the Compose stack)

Derived from `internal/localmode/assets/docker-compose.template.yml` (+ the
`docker-compose.persistence.yml` overlay). The native pack must reproduce every
live process below. **All five services are load-bearing.**

| Compose service | Image today | Role | Ports (host) | State volume | Native-mode disposition |
|---|---|---|---|---|---|
| `postgres` | `postgres:16-alpine` (`pg_stat_statements`) | primary DB + pgproxy target | 5432 | `volcano-db` | **Bundle** native PostgreSQL 16; init a private cluster under `localmode/data/postgres`; preserve `pg_stat_statements` |
| `redis` | `redis:7-alpine` | cache, streams, locks, realtime, presence | 6379 | `volcano-redis` | **Bundle** native Redis/Valkey 7 (safer for parity than reimplementing adapters) |
| `dynamodb` | `amazon/dynamodb-local` (**Java**, `-jar DynamoDBLocal.jar`) | hot-path log events only (`LOG_EVENTS_HOT_PATH_TABLE_NAME=volcano-local-log-events` via `DYNAMODB_ENDPOINT`) | 8003→8000 | `volcano-dynamodb` | **Remove.** Replace with a server-side `local`-tag log-store on the already-present PostgreSQL — see §8. Eliminates the JRE. |
| `mailpit` | `axllent/mailpit:v1.30` | **SMTP catch-all for auth email** — seeded project `auth_config.smtp_host = "mailpit"`; signup confirmation, password reset, etc. land here | 1025 (SMTP), 8025 (UI) | — | **Bundle** native Mailpit; supervise SMTP + UI ports. **Load-bearing**: dropping it breaks local signup / password-reset flows. |
| `server` | `kong/volcano:local-nightly` | the hosting API/control plane | 8000 (API), 8001 (mgmt), 8002 (pgproxy), **8080 (frontends HTTP ingress)** | `volcano-storage`, `volcano-functions` | **Native `-tags local` binary** with `ForceLocalMode=true`; supervise; embed migrations |

### Frontends HTTP ingress (do not forget)

The server exposes `FRONTENDS_HTTP_PORT=8080` with
`FRONTEND_INVOCATION_DNS=*.frontends.localhost`. Deployed frontends are reached
at `<frontend-id>.frontends.localhost:8080` (macOS resolves `*.localhost` to
`127.0.0.1` automatically). Native mode must preserve this ingress and its
wildcard-host behavior; document the resolution requirement per-OS
(Linux/Windows may need explicit `*.frontends.localhost` handling).

### Server environment (native supervisor must reproduce)

`LOCAL_MODE=true`, `DATABASE_URL` (→ bundled postgres), `REDIS_URL` (→ bundled
redis), the log-store endpoint (§8 — replaces `DYNAMODB_ENDPOINT`),
`API_BASE_URL=http://localhost:8000`, `WEB_BASE_URL=http://localhost:3000`,
`LOCAL_STORAGE_DIR`, `LOCAL_FUNCTIONS_DIR`, `PORT/MANAGEMENT_PORT/PGPROXY_PORT`,
`FRONTENDS_HTTP_PORT`, and the seeded first-party bootstrap values. Ports must
be assignable (§ supervisor, step 6) rather than hard-coded, to survive
collisions.

### Base-pack size

**TBD, pending the first `volcano-hosting` local-tagged native artifact.** The
server currently ships only as the `kong/volcano:local-nightly` OCI image
(~90 MB compressed, Linux-only); the native binary size is unknown until
hosting builds it. Do not quote the image size as the pack size.

## 8. DynamoDB Local → PostgreSQL log-store (hosting-owned, ordered prerequisite)

DynamoDB Local is used for exactly one thing in localmode: the hot-path log
events table (`LOG_EVENTS_HOT_PATH_TABLE_NAME`, reached via
`DYNAMODB_ENDPOINT`). Keeping it natively means bundling a JRE + the DynamoDB
Local jar and its native deps — which defeats the entire "dependency-free"
premise.

**Direction (agreed): remove DynamoDB Local from native localmode and back the
hot-path log store with the PostgreSQL instance already in the stack, behind a
`local` build tag.** PostgreSQL is named specifically (not "PostgreSQL/SQLite")
so there is a single source of truth: it is already a running process, so this
adds zero new dependency.

**Ownership and sequencing (merge-blocking for this doc):**

- This is a change to the **server binary**, built in `volcano-hosting`. The
  CLI can neither implement nor test it. It is therefore a
  `volcano-hosting`-owned item.
- It must land **inside hosting steps 2–4** (the local-tagged build), as a
  **blocking prerequisite** for CLI steps 5–9. A native runtime manager has
  nothing coherent to supervise until the server no longer needs DynamoDB.
- Requirements the adapter must preserve: log query semantics and ordering
  parity with the DynamoDB-backed path; the existing DynamoDB path stays intact
  for Docker localmode and all cloud tests.

This is a decision the CLI **proposes**; `volcano-hosting` implements and signs
off.

## 9. Language & tooling runtimes (on-demand packs)

Function and frontend execution need Node/npm, Python/pip, Ruby, plus `esbuild`
and `@vercel/nft`. These are **not** in the base pack.

- **On-demand, independently versioned packs**, fetched on first use, installed
  under `runtimes/localmode/language-packs/<lang>/<version>/<os>-<arch>/`.
- The CLI installs only the runtimes a project actually needs (a JS project
  never downloads Ruby).
- Same verification (§6) and GC (§11) as the base pack.

Rationale: bundling all three language ecosystems into every base pack would
balloon it into hundreds of MB and multiply signing/update/security surface for
runtimes most projects never use.

Must preserve: Node dependency installation, package-manager credentials,
runtime-version matching, function invocation behavior, and frontend
build/`next start` behavior. Detailed contract deferred to step 8's PR.

## 10. Trust & security model (required — default-deny)

**v1 provides no enforced isolation.** This is a disclosure, not a promised
boundary.

Docker gave process, filesystem, and network isolation for free. Native mode
runs user **function code and npm lifecycle scripts directly on the host with
the developer's own privileges** — that is arbitrary code execution, a
supply-chain vector on the developer's machine. The contract must be honest
about this rather than imply a sandbox it cannot deliver cross-platform.

Stated posture for v1:

- **Single-developer, single-tenant, trusted-workstation.** Running native
  localmode is equivalent, security-wise, to running the project's own code
  directly. (`internal/runtime` already frames local mode as "a single-tenant
  sandbox.")
- **Disclosure + explicit consent**: the first native start must clearly state
  that project code and its dependencies' lifecycle scripts run on the host
  without container isolation, and require opt-in.
- **No claim of isolation** anywhere in UX or docs for v1.

Best-effort OS hardening — `sandbox-exec` (macOS), `seccomp`+Landlock (Linux),
Job Objects (Windows), env-var allowlists, restricted working dirs, filesystem
boundaries, process time/memory limits, child-process cleanup — is **phased
into step 9**, and is described as hardening, never as the boundary that makes
untrusted code safe.

Also tracked under step 9: macOS signing/notarization, Windows Authenticode,
Linux package provenance, and antivirus-friendly extraction.

## 11. Retention, GC, and uninstall (CLI-owned)

A per-version, per-arch cache accumulates over the ~year this epic spans. The
CLI owns keeping it bounded:

- **Keep** the currently-selected runtime version and the previous one (for
  rollback); prune older `runtimes/localmode/<version>/` trees.
- **Language packs**: keep the versions referenced by currently-installed
  runtimes / recent projects; GC the rest on an LRU basis.
- Pruning is **opt-out-able** and never touches mutable `localmode/` state
  (data/logs/run).
- Provide an explicit uninstall path (a `runtime` subcommand in step 5) that
  removes packs and, on request, the state dirs — with confirmation.
- Never let "healthy over time" quietly become gigabytes of dead runtimes the
  user never agreed to keep.

## 12. Merge-blocking gates for this document

These two must be right before this contract merges (they are corrections, not
preferences):

1. **BOM completeness (§7):** Redis, Mailpit (auth email), and the
   `*.frontends.localhost` HTTP ingress are all present with an explicit
   native-mode disposition.
2. **Log-store relocation (§8):** framed as a `volcano-hosting`-owned, ordered
   prerequisite that gates CLI steps 5–9 — not a CLI "lock" and not a passing
   note.

Everything else (verification scheme details, size figures, manifest field
tweaks) is expected to iterate in review.

## 13. CLI ↔ runtime version compatibility

- Each CLI version pins a **compatible runtime version / manifest** (an
  immutable `local-X.Y.Z`), surfaced via `min_cli_version` / `max_cli_version`
  in the manifest (§6.3).
- On start, the CLI resolves the pinned version, reuses an already-verified
  install if present, otherwise downloads it.
- A runtime whose manifest is incompatible with the running CLI is refused with
  an actionable message (upgrade CLI or pin a matching runtime), never run
  best-effort.
- Coordinated releases: every published CLI points at an immutable, compatible
  runtime manifest.

## 14. Parity requirements with Docker localmode

Native localmode must be behaviorally equivalent to the Docker path for:

- PostgreSQL migrations and DB/storage operations.
- Realtime / Redis behavior (cache, streams, locks, presence).
- Auth emails through Mailpit.
- Log query + ordering (via the new PostgreSQL log store, §8).
- Function invocation (JS/Python/Ruby) and frontend build + `next start`.
- The frontends `*.frontends.localhost:8080` ingress.

The existing `local-nightly` image and Compose stack remain the **parity
oracle**: native bundles and the image are built from the same commit and run
against the same localmode E2E suite (step 10).

## 15. `--engine` rollout sequence (CLI-owned)

The `--engine` flag does **not** exist today and is **not** added in this
document's PR. It lands with the step-5 runtime manager. Sequence:

1. `--engine` introduced, **default `docker`**; `--engine native` is
   opt-in/experimental and errors clearly when no signed pack exists for the
   platform.
2. Opt-in config default for teams that want it.
3. Native default **per-platform**, with automatic Docker fallback, only after
   that platform passes parity (§14) with signed, durable packs.
4. Native default without fallback once parity is proven per-platform.
5. `--engine docker` retained for the whole epic for debugging and exact
   container parity.

Non-sensitive telemetry only: selected engine, install failures, startup
failures by component, platform/arch, runtime-pack version mismatch.

## 16. Phased roadmap (owners)

| Step | Deliverable | Owner | Depends on |
|---|---|---|---|
| 1 | **This contract** | volcano-cli | — |
| 2 | Cross-platform local-only server binaries (`-tags local`, `ForceLocalMode=true`) | volcano-hosting | 1 |
| 3 | Package/embed migrations with the server | volcano-hosting | 2 |
| 3b | **DynamoDB → PostgreSQL log-store adapter (§8)** | volcano-hosting | 2 |
| 4 | Publish signed, durable, versioned runtime artifacts (+ checksums, cosign, SBOM, manifest) | volcano-hosting CI | 2, 3, 3b |
| 5 | CLI runtime manager: detect, resolve, download, verify, extract atomically, reuse, upgrade/rollback, lock; `volcano runtime status/install/upgrade`; introduce `--engine` | volcano-cli | 4 |
| 6 | Native process supervisor: data/log/pid dirs, port assignment, health checks, start/stop/restart/status/clean; `volcano start --engine native` | volcano-cli | 5 |
| 7 | Bundle native data + mail services (Postgres, Redis/Valkey, Mailpit) — removes Docker for API/db/cache/logs/email | volcano-cli + volcano-hosting | 6 |
| 8 | On-demand language + tooling packs (Node/npm, Python, Ruby, esbuild, @vercel/nft) | volcano-cli + volcano-hosting | 7 |
| 9 | Harden host execution (sandboxing best-effort, signing/notarization, provenance) | volcano-cli + volcano-hosting | 6 |
| 10 | Parity + lifecycle tests (fresh/offline/interrupted/collision, cross-OS, Docker↔native) | both | 7, 8 |
| 11 | Roll out behind the `--engine` selector (§15) | volcano-cli | 10 |
| 12 | Docs + coordinated CLI/hosting releases | both | 11 |

The first genuinely useful milestone is steps 1–6 (a native server managed by
the CLI). The full "no Docker, no external dependencies" promise is not
complete until steps 7–9 cover data services, language runtimes, and
host-execution safety.

## 17. Open questions (need owning-repo sign-off)

- **Verification trust root** (§6.4): cosign keyless vs a hosting-managed key;
  key distribution to the CLI. — volcano-hosting.
- **Durable release host** (§6.2): where signed artifacts live long-term. —
  volcano-hosting CI.
- **Log-store adapter details** (§8): exact schema + ordering guarantees. —
  volcano-hosting.
- **Per-OS wildcard DNS** for `*.frontends.localhost` on Linux/Windows (§7). —
  volcano-cli (supervisor).
- **Base-pack size budget** (§7) once the first native artifact exists.
