# ATR Cloud Sessions — technical specification

Status: draft for review
Ideation: [`docs/cloud-sessions.md`](./cloud-sessions.md)
Depends on: [`docs/remote-live-view.md`](./remote-live-view.md) (`feat/rdp-live-view`)

## 1. Scope

The ideation document decided *what* to build and *where it runs*. This document specifies
*how*: package layout, type signatures, wire contracts, storage formats, manifests, and the
test plan.

**In scope.** `atr sessiond`, the session broker, the profile store, the Opal tool provider,
the container image, and the changes to `internal/remote` that let the live view be embedded
cross-origin.

**Out of scope.** `atr computer` (desktop control) in hosted sessions — it needs Xvfb in the
image and a different isolation story; see §19.7. WebRTC transport. The bring-your-own-runner
mode from the ideation doc §4.6 — the reverse-tunnel work is sketched in §7.6 only so the
broker's interfaces do not have to change later.

**Decisions carried forward without re-litigation.** Control plane on Cloud Run, data plane
as one GKE Pod per session; profile *state* export over CDP rather than directory copy; GCS
behind a `ProfileStore` interface; Proteus `Bridge` rather than `Federated`.

## 2. Component inventory

```
  NEW packages
  ├── internal/session/       session state machine, activity tracking, watchdog
  ├── internal/profile/       store, bundle format, export/import over CDP
  ├── internal/opal/          Opal tools provider: discovery, tools, resources, interactions
  └── internal/broker/        control plane: registry, scheduler, tokens, reaper
                              (built as a separate binary, cmd/atr-broker)

  NEW binaries
  ├── cmd/atr-broker/         control plane, deployed to Cloud Run
  └── (atr sessiond)          a subcommand of the existing cmd/atr binary

  CHANGED packages
  ├── internal/remote/        origin allowlist, cookie mode, runtime view-only toggle
  ├── internal/api/           readiness probe, activity hook
  ├── internal/cli/           sessiond, profile export/import subcommands
  └── internal/config/        SessionConfig, ProfileConfig

  UNCHANGED, deliberately
  └── internal/ops/           the reflection source for the Opal manifest. If a change is
                              needed here, the design is wrong.
```

`internal/ops` staying untouched is the load-bearing constraint of this spec. Every primitive
ATR already exposes over REST and MCP becomes an Opal tool with no per-primitive code, because
the schema tags are already there.

## 3. `atr sessiond`

One process, one Chrome, one user. It is the container entrypoint.

### 3.1 Interface

```
atr sessiond \
  --session-id      s_01HQ...          # required, from the broker
  --owner           u_123              # required, bound into every token check
  --broker          https://broker...  # required, for heartbeat and lifecycle
  --profile-uri     gs://.../p_7.tar.zst   # empty = fresh profile
  --profile-key     projects/.../cryptoKeys/tenant-42
  --idle-timeout    20m
  --max-lifetime    4h
  --port            8080               # single port: REST + live view + health
  --allow-origin    https://opal.example.com
  --jwks-url        https://broker.../.well-known/jwks.json
```

Flags mirror `internal/cli/browser.go` conventions and every one is also readable from
`ATR_SESSION_*` environment variables, because a k8s manifest sets env, not argv.

### 3.2 Startup

```
  sessiond
     │
     ├─ 1. resolve config (flags → env → ~/.atr/config.yaml)
     │
     ├─ 2. profile restore                         ── --profile-uri empty ─▶ mkdir /profile
     │      ├─ ProfileStore.Get(ctx, uri)
     │      ├─ KMS decrypt DEK, AES-GCM decrypt payload
     │      ├─ zstd -d | tar -x → /profile
     │      └─ verify manifest checksum            ── mismatch ─▶ fall back to previous gen
     │
     ├─ 3. api.NewServer(...) + browser.Launch()   Chrome on --user-data-dir=/profile
     │
     ├─ 4. remote: Discover(cdp) → NewHub → NewStreamer → Attach → Select("")
     │
     ├─ 5. profile bundle import (if the bundle carries cookies/storage)
     │      Network.setCookies, Storage.setLocalStorage… over CDP
     │
     ├─ 6. mount one mux on :8080
     │      /api/v1/*      → existing api.Server handlers
     │      /live/*        → remote.Server.Handler()
     │      /healthz       → liveness   (process up)
     │      /readyz        → readiness  (Chrome up, profile restored, streamer attached)
     │      /internal/*    → broker-only: activity, takeover, checkpoint, drain
     │
     ├─ 7. goroutines
     │      ├─ streamer.Watch(ctx)         (exists)
     │      ├─ watchdog.Run(ctx)           idle + max-lifetime
     │      ├─ heartbeat.Run(ctx)          POST broker every 30s
     │      └─ checkpointer.Run(ctx)       every 5 min while active
     │
     └─ 8. announce READY to the broker, then serve
```

Step 5 runs *after* Chrome is up because cookie injection is a CDP call, not a file write.
That is the whole reason the export-the-state approach works across operating systems.

### 3.3 Shutdown

```go
// internal/session/lifecycle.go
//
// Order matters. Chrome must be closed before the tar, or the SQLite files in
// the profile are captured mid-write and the checkpoint is unusable.
func (s *Session) Shutdown(ctx context.Context, reason Reason) error {
    s.setState(StateStopping, reason)
    s.remote.SetViewOnly(true)          // stop accepting human input
    s.api.Drain()                    // reject new commands with 503
    s.streamer.Close()               // detach the second CDP session
    if err := s.browser.Close(); err != nil {
        log.Warn("browser close failed; checkpoint may be stale", "err", err)
    }
    if err := s.checkpoint(ctx, CheckpointFinal); err != nil {
        return fmt.Errorf("final checkpoint failed: %w", err)
    }
    return s.broker.Terminated(ctx, s.id, reason)
}
```

`terminationGracePeriodSeconds: 60` in the manifest. Measured budget: browser close ~1 s,
tar+zstd of a 40 MB profile ~2 s, encrypt+upload ~3 s. The margin absorbs a slow profile.

## 4. `internal/session`

### 4.1 Types

```go
package session

type State string

const (
    StatePending    State = "PENDING"
    StateReady      State = "READY"
    StateExpiring   State = "EXPIRING"   // warned, still usable
    StateStopping   State = "STOPPING"
    StateTerminated State = "TERMINATED"
    StateFailed     State = "FAILED"
)

type Reason string

const (
    ReasonIdle        Reason = "idle_timeout"
    ReasonMaxLifetime Reason = "max_lifetime"
    ReasonUserEnded   Reason = "user_ended"
    ReasonAgentEnded  Reason = "agent_ended"
    ReasonEvicted     Reason = "evicted"
    ReasonStartFailed Reason = "start_failed"
)

// Session is the in-Pod view. The broker holds the authoritative record; this
// struct is what the process itself knows.
type Session struct {
    ID          string
    Owner       string
    TenantID    string
    ProfileID   string

    state       atomic.Value  // State
    lastActive  atomic.Int64  // unix nanos
    startedAt   time.Time

    IdleTimeout time.Duration
    MaxLifetime time.Duration
    // ...wired components: api, remote, streamer, browser, store, broker
}
```

### 4.2 Activity

The ideation doc settled which signals count. This is the enforcement point:

```go
type ActivityKind uint8

const (
    ActivityToolCall  ActivityKind = iota // agent command via /api/v1/*
    ActivityHumanInput                    // mouse, key, navigate from the live view
    ActivityViewerAttach                  // a viewer connected or is still connected
)

// Touch records activity. Frames produced by a self-refreshing page are
// deliberately not an activity source — a dashboard on a 5s refresh would
// otherwise pin a session open forever.
func (s *Session) Touch(k ActivityKind) {
    s.lastActive.Store(time.Now().UnixNano())
    if s.State() == StateExpiring {
        s.setState(StateReady, "")
        s.remote.BroadcastControl(map[string]any{"t": "sessionRenewed"})
    }
}
```

Three call sites, all thin:

| Signal | Hook |
|---|---|
| `ActivityToolCall` | middleware wrapping `api.Server.mux`, before dispatch |
| `ActivityHumanInput` | `remote.Server.dispatch`, on `mouse`/`wheel`/`key`/`text`/`navigate` |
| `ActivityViewerAttach` | ticker over `hub.Count() > 0` — attach alone is not enough, the viewer must still be connected on each tick |

### 4.3 Watchdog

```go
// Run ticks every 10s. It warns at IdleTimeout-3m and self-terminates at
// IdleTimeout. The broker reaper is the authority (§7.5) — this watchdog is
// the fast, clean path, not the guarantee.
func (w *Watchdog) Run(ctx context.Context) {
    t := time.NewTicker(10 * time.Second)
    defer t.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-t.C:
            idle := w.s.IdleFor()
            age  := time.Since(w.s.startedAt)

            switch {
            case age >= w.s.MaxLifetime:
                w.s.Shutdown(ctx, ReasonMaxLifetime)
                return
            case idle >= w.s.IdleTimeout:
                w.s.Shutdown(ctx, ReasonIdle)
                return
            case idle >= w.s.IdleTimeout-warnBefore && w.s.State() == StateReady:
                w.s.setState(StateExpiring, ReasonIdle)
                w.s.remote.BroadcastControl(map[string]any{
                    "t": "sessionExpiring",
                    "expiresAt": time.Now().Add(w.s.IdleTimeout - idle).UTC(),
                })
            }
        }
    }
}

const warnBefore = 3 * time.Minute
```

`sessionExpiring` joins the existing control-message vocabulary on the live-view WebSocket
(`pages`, `status`), so the React client handles it with the switch it already has.

## 5. `internal/profile`

### 5.1 Store interface

```go
package profile

// Store is the persistence seam. GCS is the shipping backend; S3 exists so a
// multi-cloud decision later is a constructor change, not a refactor.
type Store interface {
    Get(ctx context.Context, uri string) (io.ReadCloser, *Manifest, error)
    Put(ctx context.Context, uri string, r io.Reader, m *Manifest) error
    List(ctx context.Context, tenant, user string) ([]Manifest, error)
    Delete(ctx context.Context, uri string) error

    // Lease guards a profile against concurrent use. Chrome corrupts a
    // user-data-dir shared by two processes, so this is correctness, not
    // politeness.
    Lease(ctx context.Context, profileID, sessionID string, ttl time.Duration) (Lease, error)
}

type Lease interface {
    Renew(ctx context.Context) error
    Release(ctx context.Context) error
}
```

Backends: `profile.NewGCSStore(bucket, kms)`, `profile.NewS3Store(...)`,
`profile.NewFileStore(dir)` for tests and local development.

### 5.2 Bundle format

```
  atr-profile.bundle  (a tar, zstd level 10, then AES-256-GCM)

  manifest.json          unencrypted header, 4-byte length prefix
  payload.enc            everything below, encrypted with the DEK

    dir/                 the stripped user-data-dir (see ideation §7.1)
      Default/Preferences
      Default/Bookmarks
      Local State
      ...
    state/
      cookies.json       [{name, value, domain, path, expires, httpOnly,
                           secure, sameSite}]  ← CDP Network.getAllCookies shape
      localStorage.json  {origin: {key: value}}
      indexeddb/         per-origin dumps, optional
```

```go
type Manifest struct {
    Version      int       `json:"version"`       // 1
    ProfileID    string    `json:"profile_id"`
    TenantID     string    `json:"tenant_id"`
    OwnerID      string    `json:"owner_id"`
    CreatedAt    time.Time `json:"created_at"`
    Origins      []string  `json:"origins"`        // exactly what moved — the audit record
    ChromeMajor  int       `json:"chrome_major"`
    SourceOS     string    `json:"source_os"`      // darwin | windows | linux
    Bytes        int64     `json:"bytes"`
    SHA256       string    `json:"sha256"`         // of payload.enc
    WrappedDEK   []byte    `json:"wrapped_dek"`    // KMS-wrapped, per-user
    Generation   int       `json:"generation"`
}
```

`Origins` is not metadata for its own sake. When someone asks which of their logins were
copied into the cloud, this field is the answer, and §5.4 refuses to write a bundle whose
contents do not match it.

### 5.3 Storage layout and lifecycle

```
  gs://atr-profiles-{env}/
    {tenant_id}/{sha256(user_id)[:16]}/{profile_id}/
      manifest.json
      profile-{generation}.tar.zst.enc      current
      profile-{generation-1}.tar.zst.enc    rollback for a corrupt checkpoint
```

- Per-user DEK, wrapped by a per-tenant KEK in Cloud KMS. A bucket misconfiguration then
  leaks ciphertext, and a tenant's keys revoke independently.
- Bucket: uniform access, public access prevention, versioning off (generations are explicit),
  lifecycle rule deleting objects untouched for `profile.retention_days` (default 30).
- The Pod's service account gets `roles/storage.objectAdmin` on its own prefix only, via a
  Workload-Identity-bound condition on the object prefix.

### 5.4 Export — client side

```
atr profile export --domains example.com,corp.example.net --out work.bundle
```

```
  user's own Chrome                     atr profile export
        │                                       │
        │◀── attach over CDP (or launch) ───────┤
        │                                       │
        │  Network.getAllCookies                │  Chrome decrypts with the OS
        ├──────────────────────────────────────▶│  keychain. We never see the key.
        │                                       │
        │  Runtime.evaluate per origin:         │
        │    localStorage snapshot              │
        ├──────────────────────────────────────▶│
        │                                       │
        │                          filter to --domains   ◀── scope, do not vacuum
        │                          strip caches from dir/
        │                          tar | zstd | AES-GCM
        │                                       └──▶ work.bundle
```

`--domains` is required. There is no "export everything" flag, and that is deliberate: the
default must not be "ship every session this person holds to a server".

Cookie domain matching follows the cookie spec, not string prefix — `corp.example.net` must
not match `evilcorp.example.net`, and a `--domains example.com` must include `.example.com`
subdomain cookies. This is the one piece of §5 with real edge cases; it gets a table test.

### 5.5 Import — Pod side

```go
// Import injects exported state into a running browser. It runs after Chrome
// is up because cookies land through CDP, not through the filesystem — that is
// what makes a macOS-exported bundle work on Linux.
func Import(ctx context.Context, b *browser.Browser, bundle *Bundle) (ImportResult, error)
```

Reports per-origin success so a partial import is visible rather than mysterious:

```json
{ "cookies_set": 84, "origins_restored": ["https://corp.example.net"],
  "origins_failed": [{"origin":"https://legacy.example.com","reason":"invalid cookie domain"}] }
```

## 6. Changes to existing packages

### 6.1 `internal/remote` — origin allowlist

`checkOrigin` currently accepts loopback only, so an Opal iframe's WebSocket upgrade is
refused. See `internal/remote/server.go:71`.

```go
// NewServer gains an allowedOrigins parameter. Empty keeps today's behaviour.
func (s *Server) checkOrigin(r *http.Request) bool {
    origin := r.Header.Get("Origin")
    if origin == "" {
        return true // a non-browser client, such as a test
    }
    u, err := url.Parse(origin)
    if err != nil {
        return false
    }
    host, _, err := net.SplitHostPort(u.Host)
    if err != nil {
        host = u.Host
    }
    if isLoopbackHost(host) {
        return true
    }
    for _, allowed := range s.allowedOrigins {
        // Compare the full origin, scheme included. Host-only comparison would
        // accept http:// against an https:// allowlist entry.
        if strings.EqualFold(origin, allowed) {
            return true
        }
    }
    return false
}
```

CLI: `--allow-origin` (repeatable) on `atr remote`, `ATR_RDP_ALLOW_ORIGIN` as a comma list.

### 6.2 `internal/remote` — cookie mode

The auth cookie is `SameSite=Strict` (`internal/remote/server.go:117`). Browsers do not send a
`Strict` cookie from a cross-origin iframe, so the SPA would load and then every asset request
and the WebSocket upgrade would 401.

```go
type CookieMode int

const (
    CookieLoopback CookieMode = iota // SameSite=Strict — today's default
    CookieEmbedded                   // SameSite=None; Secure; Partitioned
)

func (s *Server) setAuthCookie(w http.ResponseWriter) {
    c := &http.Cookie{Name: cookieName, Value: s.token, Path: "/", HttpOnly: true}
    if s.cookieMode == CookieEmbedded {
        c.SameSite = http.SameSiteNoneMode
        c.Secure = true
        // Partitioned (CHIPS) is not in net/http's Cookie struct; append it to
        // the raw attribute list so third-party cookie phase-out does not
        // silently break the embed.
        c.Raw = c.String() + "; Partitioned"
    } else {
        c.SameSite = http.SameSiteStrictMode
    }
    http.SetCookie(w, c)
}
```

Embedded mode requires TLS. `sessiond` refuses `CookieEmbedded` without it rather than setting
a `Secure` cookie that the browser will drop.

### 6.3 `internal/remote` — runtime view-only

`viewOnly` is fixed at construction. Takeover needs it per-session and mutable:

```go
func (s *Server) SetViewOnly(v bool)  // atomic.Bool; dispatch reads it per message
func (s *Server) ViewOnly() bool
```

While a human holds takeover, agent commands to `/api/v1/*` return `409 human_has_control`
with the holder and an expiry. Two actors driving one Chrome produces races that read as
flaky tests, which is the failure mode this exists to prevent.

### 6.4 `internal/api` — readiness and activity

```go
s.mux.HandleFunc("/api/v1/health", s.handleHealth)   // exists
s.mux.HandleFunc("/readyz", s.handleReady)           // NEW
```

`/readyz` returns 200 only when the browser is launched, CDP answers, and — in a hosted
session — the profile restore has completed. Liveness must not depend on Chrome; a wedged
Chrome should be reported, not restarted underneath a live profile.

The activity middleware wraps the mux in `sessiond` only, so the standalone daemon is
unchanged.

## 7. Control plane — `cmd/atr-broker`

### 7.1 API

All endpoints require an Opal-issued caller identity. `{session_id}` is resolved to its owner
and compared against the caller on every call; a session ID is never a capability on its own.

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/sessions` | Create or resume. Body: `{profile_id?, purpose?, idle_timeout?}` |
| `GET` | `/v1/sessions/{id}` | State, idle remaining, URL, viewer count |
| `POST` | `/v1/sessions/{id}/extend` | Reset the idle clock |
| `POST` | `/v1/sessions/{id}/takeover` | Grant human control, `{hold: "5m"}` |
| `POST` | `/v1/sessions/{id}/release` | Return control to the agent |
| `DELETE` | `/v1/sessions/{id}` | Checkpoint and terminate |
| `POST` | `/v1/sessions/{id}/token` | Mint a 5-minute live-view JWT |
| `GET` | `/v1/profiles` | List the caller's profiles |
| `POST` | `/v1/profiles/import` | Signed upload URL for a bundle |
| `POST` | `/internal/sessions/{id}/heartbeat` | Pod → broker, every 30 s |
| `POST` | `/internal/sessions/{id}/state` | Pod → broker, state transitions |
| `POST` | `/internal/reap` | Cloud Scheduler, every 60 s |

`POST /v1/sessions` response:

```json
{ "session_id": "s_01HQ8...",
  "state": "READY",
  "profile_id": "p_work",
  "live_view_url": "https://sessions.atr.example.com/s_01HQ8.../live/?t=<jwt>",
  "expires_at": "2026-08-22T14:05:00Z",
  "idle_timeout_seconds": 1200 }
```

### 7.2 Registry

Firestore, collection `sessions`, document id = session id:

| Field | Type | Note |
|---|---|---|
| `owner_id`, `tenant_id` | string | binding for every authorisation check |
| `profile_id` | string | empty for an ephemeral session |
| `state` | string | §4.1 |
| `pod_name`, `pod_ip`, `namespace` | string | routing target |
| `created_at`, `last_activity_at` | timestamp | |
| `expires_at` | timestamp | **indexed** — the reaper's only query |
| `max_lifetime_at` | timestamp | absolute cap |
| `takeover_holder`, `takeover_until` | string, timestamp | |
| `terminated_reason` | string | |

Composite index on `(state, expires_at)`. The reaper query must stay a single indexed range
scan; at ten thousand sessions a collection scan every 60 seconds is not viable.

Profile leases live in `profile_leases`, document id = profile id, with `session_id` and
`expires_at`. Acquisition is a Firestore transaction — the only correct way to make
"one live session per profile" hold under a concurrent double `atr_session_start`.

### 7.3 Routing

The broker is a reverse proxy for the data plane. `sessions.atr.example.com/{session_id}/*`
resolves the session, verifies the caller, and proxies to `http://{pod_ip}:8080/*`, including
the WebSocket upgrade.

Proxying rather than exposing Pods directly means one TLS certificate, one place that enforces
`owner == caller`, one audit log, and no Pod ever reachable from the internet.

### 7.4 Tokens

```json
{ "iss": "atr-broker", "sub": "u_123", "sid": "s_01HQ8...",
  "aud": "atr-session", "tid": "t_42", "scope": "live:input",
  "iat": 1755864000, "exp": 1755864300 }
```

Five-minute TTL, `RS256`, JWKS at `/.well-known/jwks.json`, verified in the Pod. The live-view
client refreshes through the broker before expiry. A short TTL is what makes a live-view URL
pasted into a bug report or captured in a screenshot harmless within minutes.

`scope` is `live:view` under a takeover held by someone else, `live:input` otherwise.

### 7.5 Reaper

```
  Cloud Scheduler ──60s──▶ POST /internal/reap
                              │
                              ├─ query: state IN (READY, EXPIRING, PENDING)
                              │         AND expires_at < now        ← indexed
                              │
                              └─ for each, in parallel, bounded at 20:
                                   ├─ DELETE Pod (grace 60s)
                                   ├─ release the profile lease
                                   └─ state = TERMINATED, reason = idle_timeout
```

`expires_at` is `last_activity_at + idle_timeout`, written on every heartbeat. A Pod that
wedges stops heartbeating, `expires_at` goes stale, and the reaper removes it — which is
precisely the case the in-Pod watchdog cannot handle and the reason both exist.

Reaping is idempotent: a Pod already gone is a successful reap.

### 7.6 Scheduling

`POST /v1/sessions` creates a Pod from a template (§8). A **warm pool** of blank Pods in
`state=WARM` with no owner absorbs the cold start: a fresh-profile session claims one and is
ready in ~2 s; a profile-restore session pays only the restore. Pool size is
`max(2, ceil(0.1 × active))`, refilled asynchronously.

The scheduler is behind an interface so the VM fallback and the reverse-tunnel mode from the
ideation doc do not require broker surgery:

```go
type Scheduler interface {
    Start(ctx context.Context, spec SessionSpec) (Placement, error)
    Stop(ctx context.Context, p Placement, grace time.Duration) error
    Get(ctx context.Context, p Placement) (PlacementStatus, error)
}
// implementations: GKEScheduler, MIGScheduler, TunnelScheduler
```

## 8. Data plane manifest

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: atr-session-{session_id}
  labels: { app: atr-session, session-id: "{session_id}", owner-hash: "{sha256(owner)[:16]}" }
spec:
  terminationGracePeriodSeconds: 60
  automountServiceAccountToken: false      # the Pod has no business calling the k8s API
  serviceAccountName: atr-session          # Workload Identity → its own GCS prefix only
  runtimeClassName: gvisor                 # renders untrusted content by definition
  securityContext:
    runAsNonRoot: true
    runAsUser: 10001
    seccompProfile: { type: RuntimeDefault }
  containers:
    - name: sessiond
      image: {registry}/atr-session:{version}
      args: ["sessiond"]
      env:
        - { name: ATR_SESSION_ID,      value: "{session_id}" }
        - { name: ATR_SESSION_OWNER,   value: "{owner_id}" }
        - { name: ATR_SESSION_BROKER,  value: "https://broker.internal" }
        - { name: ATR_SESSION_PROFILE_URI, value: "{profile_uri}" }
        - { name: ATR_SESSION_IDLE_TIMEOUT, value: "20m" }
        - { name: ATR_RDP_ALLOW_ORIGIN, value: "https://opal.example.com" }
      ports: [{ containerPort: 8080 }]
      resources:
        requests: { cpu: "500m",  memory: "1536Mi" }
        limits:   { cpu: "2000m", memory: "3Gi" }
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities: { drop: ["ALL"] }
      volumeMounts:
        - { name: profile, mountPath: /profile }
        - { name: tmp,     mountPath: /tmp }
        - { name: shm,     mountPath: /dev/shm }
      livenessProbe:  { httpGet: { path: /healthz, port: 8080 }, periodSeconds: 10 }
      readinessProbe: { httpGet: { path: /readyz,  port: 8080 }, periodSeconds: 5,
                        failureThreshold: 24 }   # 120s for a large profile restore
  volumes:
    - { name: profile, emptyDir: { sizeLimit: 4Gi } }
    - { name: tmp,     emptyDir: { sizeLimit: 1Gi } }
    - { name: shm,     emptyDir: { medium: Memory, sizeLimit: 1Gi } }   # Chrome needs it
```

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: { name: atr-session-egress }
spec:
  podSelector: { matchLabels: { app: atr-session } }
  policyTypes: [Egress, Ingress]
  ingress:
    - from: [{ podSelector: { matchLabels: { app: atr-broker-proxy } } }]
      ports: [{ port: 8080 }]
  egress:
    - to:
        - ipBlock:
            cidr: 0.0.0.0/0
            except:
              - 10.0.0.0/8
              - 172.16.0.0/12
              - 192.168.0.0/16
              - 169.254.0.0/16      # metadata server. Without this line the
                                    # model does not hold: a hostile page can
                                    # mint the node's service-account token.
    - to: [{ namespaceSelector: { matchLabels: { kube-system: "true" } } }]
      ports: [{ port: 53, protocol: UDP }]
```

The `169.254.0.0/16` exclusion is the single most important line in this specification. A CI
check asserts its presence in the rendered manifest, because it is easy to lose in a refactor
and its absence is silent until it is catastrophic.

## 9. Opal provider — `internal/opal`

### 9.1 Discovery generated from `internal/ops`

`internal/mcp/schema.go` already reflects `ops.XRequest` structs into JSON Schema using
`jsonschema:"required"` and `jsonschema_description:"..."` tags. The Opal manifest reuses that
reflection verbatim; the only addition is a `session_id` property injected into every
session-scoped tool.

```go
// internal/opal/discovery.go
//
// Tool schemas come from the same reflection MCP uses, so a new primitive in
// internal/ops appears in Opal without a line of Opal-specific code.
func toolSchema(req any) map[string]any {
    s := schemaFor(req)              // shared with internal/mcp
    props := s["properties"].(map[string]any)
    props["session_id"] = map[string]any{
        "type": "string",
        "description": "Session returned by atr_session_start",
    }
    s["required"] = append(toStrings(s["required"]), "session_id")
    return s
}
```

`GET /discovery` returns `application/vnd.optimizely.opal.tools.discovery+json`:

```json
{ "functions": [
  { "name": "atr_session_start",
    "description": "Start a hosted browser session and show its live view",
    "parameters": { "type": "object", "properties": {
        "profile_id": {"type":"string","description":"Stored profile to load; omit for a fresh browser"},
        "purpose":    {"type":"string","description":"What this session is for; shown to the user"} } },
    "ui_resource": "ui://atr/live-view" },
  { "name": "atr_navigate",
    "description": "Navigate the session's browser to a URL",
    "parameters": { "type":"object",
      "properties": { "session_id": {"type":"string"}, "url": {"type":"string"} },
      "required": ["session_id","url"] },
    "ui_resource": "ui://atr/live-view" }
] }
```

Every session-scoped tool carries `ui_resource`, so TMS attaches a `ui_interaction` callback
to all of them and the live view can re-render after any command.

### 9.2 Tool catalogue

| Tool | Source |
|---|---|
| `atr_session_start`, `atr_session_status`, `atr_session_extend`, `atr_session_end` | hand-written, §7.1 |
| `atr_profile_list`, `atr_profile_import` | hand-written |
| `atr_navigate`, `atr_click`, `atr_fill`, `atr_hover`, `atr_press_key`, `atr_scroll`, `atr_snapshot`, `atr_screenshot`, `atr_text`, `atr_html`, `atr_eval`, `atr_wait`, `atr_console`, `atr_errors`, … | reflected from `internal/ops` |

No `atr_exec`. A generic command-string tool is a remote shell wearing a tool schema; it is
unreviewable and it destroys the per-command audit record in §11.

### 9.3 Resources and interactions

`POST /resources/read` with `{"uri": "ui://atr/live-view"}` returns the Proteus document. TMS
caches resource content for 30 minutes, so the document must be **static** — session-specific
values arrive through the tool result's data, not baked into the resource.

```json
{ "$type": "Document", "appName": "ATR",
  "title":    { "$type": "Value", "path": "/title" },
  "subtitle": { "$type": "Value", "path": "/subtitle" },
  "body": [
    { "$type": "Bridge",
      "resource": "ui://atr/live-view-frame",
      "height": 720,
      "fallback": { "$type": "Alert",
        "children": "Open this conversation in Opal web to view the browser." } },
    { "$type": "Group", "flexDirection": "row", "gap": "8", "children": [
      { "$type": "Badge", "children": { "$type": "Value", "path": "/state" } },
      { "$type": "Text",  "children": { "$type": "Value", "path": "/current_url" } } ] }
  ],
  "actions": [
    { "$type": "Action", "appearance": "primary", "children": "Take over",
      "onClick": { "interaction": "atr_takeover",
                   "params": { "session_id": { "$type": "Value", "path": "/session_id" } } } },
    { "$type": "Action", "children": "Extend 20 min",
      "onClick": { "interaction": "atr_extend",
                   "params": { "session_id": { "$type": "Value", "path": "/session_id" } } } },
    { "$type": "Action", "appearance": "danger", "children": "End session",
      "onClick": { "interaction": "atr_end",
                   "params": { "session_id": { "$type": "Value", "path": "/session_id" } } } }
  ] }
```

`POST /interactions/execute` routes on `name`: `atr_takeover`, `atr_release`, `atr_extend`,
`atr_end`, `atr_upload_profile`. Each re-derives the caller from the TMS-forwarded auth
context and re-checks ownership — an interaction is a request from a browser, not a trusted
internal call.

### 9.4 Full call path

```
 agent    TMS      opal provider   broker    Firestore   GKE     Pod    user's tab
   │       │             │            │          │        │       │        │
   ├ tool ▶│             │            │          │        │       │        │
   │       ├─ /tools/ ──▶│            │          │        │       │        │
   │       │  atr_session│            │          │        │       │        │
   │       │  _start     ├─ POST ────▶│          │        │       │        │
   │       │             │  /sessions ├─ lease ─▶│        │       │        │
   │       │             │            ├─ claim warm pod ─▶│       │        │
   │       │             │            │          │        ├─ start▶│        │
   │       │             │            │◀───────── READY ──────────┤        │
   │       │             │◀─ session ─┤          │        │       │        │
   │       │◀─ result ───┤  + jwt     │          │        │       │        │
   │◀──────┤  data +     │            │          │        │       │        │
   │       │  ui_resource│            │          │        │       │        │
   │       │             │            │          │        │       │        │
   │       │  frontend fetches ui_resource, renders Bridge iframe │        │
   │       │─────────────────────────────────────────────────────────────▶│
   │       │             │            │          │        │       │        │
   │       │             │       iframe: GET /{sid}/live/?t=jwt   │        │
   │       │             │            │◀───────────────────────────────────┤
   │       │             │            ├─ verify owner, proxy ────▶│        │
   │       │             │            │          │        │       ├─ WS ──▶│
   │       │             │            │          │        │       │ frames │
```

## 10. Configuration

```yaml
# ~/.atr/config.yaml
session:
  idle_timeout: 20m
  max_lifetime: 4h
  warn_before: 3m
  checkpoint_interval: 5m
  broker_url: ""
  allow_origins: []

profile:
  store: gcs                      # gcs | s3 | file
  bucket: atr-profiles-prod
  kms_key: projects/p/locations/l/keyRings/r/cryptoKeys/tenant-default
  retention_days: 30
  max_bytes: 524288000            # 500 MB; reject a bundle above this
```

New structs in `internal/config/config.go` following the existing `mapstructure` pattern, with
`ATR_SESSION_*` / `ATR_PROFILE_*` bindings registered alongside the existing `BindEnv` calls.

## 11. Errors

Returned as RFC 9457 `ProblemDetail`, which the Opal SDK already models
(`sdks/python/opal_tools_sdk/response.py`).

| `error_code` | HTTP | When | What the agent should do |
|---|---|---|---|
| `session_expired` | 410 | Idle or max-lifetime reap | Call `atr_session_start` again |
| `session_not_found` | 404 | Unknown id | Start a session; do not retry |
| `session_forbidden` | 403 | Caller is not the owner | Stop; this is a bug or an attack |
| `session_starting` | 409 | Still `PENDING` | Retry with backoff, ≤60 s |
| `human_has_control` | 409 | Takeover held | Wait, or ask the person to release |
| `profile_locked` | 409 | Another live session holds the lease | Offer to reuse or end that session |
| `profile_too_large` | 413 | Bundle above `max_bytes` | Re-export with fewer domains |
| `profile_restore_failed` | 500 | Both generations unusable | Start fresh; the profile needs attention |
| `capacity_exhausted` | 503 | Tenant quota or cluster full | Retry with backoff |

Distinguishing `session_expired` (410) from `session_not_found` (404) matters more than it
looks: an agent that cannot tell "timed out" from "wrong id" retries the wrong thing, and the
20-minute timeout is a *routine* event, not an error condition.

Structured warning on successful results during `EXPIRING`:

```json
{ "data": {...},
  "warning": { "code": "session_expiring",
               "message": "Session s_01HQ8 expires in 3 minutes; call atr_session_extend to keep it",
               "expires_at": "2026-08-22T14:05:00Z" } }
```

## 12. Observability

Prometheus, `atr_session_*`:

| Metric | Type | Purpose |
|---|---|---|
| `sessions_active{tenant,state}` | gauge | capacity and leaks |
| `session_start_seconds{profile:fresh\|restored}` | histogram | the warm-pool SLO |
| `session_duration_seconds{reason}` | histogram | is 20 minutes right (§19.4) |
| `session_terminated_total{reason}` | counter | idle vs user vs evicted mix |
| `profile_checkpoint_seconds`, `profile_bytes` | histogram | grace-period budget |
| `profile_restore_failures_total{generation}` | counter | corruption rate |
| `remote_frames_sent_total`, `remote_bytes_sent_total` | counter | the egress bill (ideation §12) |
| `remote_viewers` | gauge | drives stream-only-when-watched |
| `reaper_lag_seconds` | histogram | how long past `expires_at` a session survives |

Every log line in a session Pod carries `session_id`, `owner_id`, `tenant_id`. Command audit
is a separate structured stream — `(ts, user, session, tool, args_digest, target_origin,
result)` — retained per the tenant's policy. "What did the agent do with my Salesforce
session" must have an answer.

## 13. Container image

No Dockerfile exists in the repo today; this is new.

```dockerfile
# Stage 1: web assets — the Makefile's `web` target already does this
FROM node:22-slim AS web
WORKDIR /w
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: Go
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /w/dist ./web/dist
RUN CGO_ENABLED=1 go build -ldflags "-s -w" -o /out/atr ./cmd/atr

# Stage 3: runtime
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      chromium fonts-liberation fonts-noto-color-emoji ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*
RUN useradd -u 10001 -m atr
COPY --from=build /out/atr /usr/local/bin/atr
USER 10001
# The names ATR already uses — see internal/cli/browser.go.
ENV ATR_BEHAVIOR_BROWSER_EXECUTABLE=/usr/bin/chromium \
    ATR_BEHAVIOR_BROWSER_HEADLESS=false \
    ATR_BEHAVIOR_BROWSER_NO_SANDBOX=true \
    ATR_BEHAVIOR_BROWSER_PERSIST_SESSION=true \
    ATR_BEHAVIOR_BROWSER_DATA_DIR=/profile
ENTRYPOINT ["/usr/local/bin/atr"]
CMD ["sessiond"]
```

`HEADLESS=false` is not a mistake. The live view uses CDP's screencast, which needs a real
compositor; `--headless=new` produces frames but diverges on rendering details that matter for
the visual checks ATR already does. A headless-shell image is smaller — measure both before
choosing.

`CGO_ENABLED=1` because `internal/computer` pulls robotgo. If desktop control is genuinely out
of scope for hosted sessions (§19.7), a build tag that excludes it yields a static binary and
a much smaller image — worth doing, and it is the same tag that decides whether Xvfb belongs
in the image.

Pin Chromium and assert at startup that its major version matches the profile manifest's
`chrome_major`; a profile written by a much newer Chrome may not load in an older one.

## 14. Test plan

| Layer | What | Where |
|---|---|---|
| Unit | State machine transitions, every edge including `EXPIRING → READY` on late activity | `internal/session` |
| Unit | Watchdog with an injected clock: warn at 17 m, terminate at 20 m, max-lifetime wins over idle | `internal/session` |
| Unit | Bundle round-trip: tar/zstd/encrypt/decrypt, checksum mismatch, generation fallback | `internal/profile` |
| Unit | **Cookie domain matching** — `corp.example.net` must not match `evilcorp.example.net`; `.example.com` subdomain cookies must be included by `--domains example.com` | `internal/profile` |
| Unit | `checkOrigin`: loopback default, allowlist hit, scheme mismatch rejected, absent Origin | `internal/remote` |
| Unit | Cookie mode: `Strict` for loopback, `None; Secure; Partitioned` for embedded, refusal without TLS | `internal/remote` |
| Unit | Reflected Opal schema equals the MCP schema plus `session_id` | `internal/opal` |
| Integration | Real Chrome: export cookies → fresh profile → import → assert logged in against a local fixture server | `internal/profile` |
| Integration | Full session lifecycle against a fake scheduler and an in-memory store | `internal/broker` |
| Integration | Concurrent double `atr_session_start` on one profile → exactly one wins, the other gets `profile_locked` | `internal/broker` |
| Integration | Ownership: user B calling every session endpoint with user A's session id gets 403 | `internal/broker` |
| Manifest | Rendered NetworkPolicy contains the `169.254.0.0/16` exclusion | CI |
| Manifest | Pod spec has `automountServiceAccountToken: false`, non-root, read-only rootfs | CI |
| E2E | Opal staging: `atr_session_start` → Bridge renders → human takeover completes a login → agent continues → idle 20 m → `session_expired` → restart resumes the profile | staging |
| Load | 50 concurrent sessions, 10 watched: cold start p95, egress, reaper lag | staging |

CI already runs `go test -race ./...`; the integration tests needing Chrome go behind the same
skip pattern as the existing browser tests (`internal/browser` skips on headless runners).

## 15. Rollout

1. Ship behind `session.enabled=false`. The Opal service registers in a staging org only.
2. Internal dogfood, 10 users, fresh profiles only, profile import disabled.
3. Enable profile *checkpointing* (fresh profiles persist). Watch
   `profile_restore_failures_total` for a week.
4. Enable `atr profile export`/`import` only after the §19.2 security review, and only for
   tenants that have opted in with domain scoping.
5. General availability with per-tenant concurrent-session quotas.

Kill switches: `session.enabled` stops new sessions without touching live ones;
`profile.import_enabled` is independent, because import is the piece most likely to need to be
withdrawn quickly.

## 16. Phases

| Phase | Content | PR shape | Result |
|---|---|---|---|
| 0 | §6.1, §6.2, §6.4 — origin allowlist, cookie mode, `/readyz` | 1 small PR to `feat/rdp-live-view` | The live view can be embedded cross-origin |
| 1 | `internal/session` + `atr sessiond` + Dockerfile, fresh profiles, no broker | 2 PRs | A hosted session driven by REST |
| 2 | `internal/broker`: registry, scheduler, tokens, proxy | 2 PRs | Multi-user, isolated, addressable |
| 3 | `internal/opal`: discovery, tools, resources, interactions | 2 PRs | The agent starts a session; the person sees it in the chat |
| 4 | `internal/profile`: store, checkpoint, restore, leases | 2 PRs | Sessions remember their logins |
| 5 | Watchdog, reaper, expiry warnings, error catalogue | 1 PR | R9, R10 |
| 6 | `atr profile export`/`import`, domain-scoped | 2 PRs | R2, the version that works |
| 7 | Takeover, stream-only-when-watched, adaptive quality, gVisor, NetworkPolicy CI | 2 PRs | Production |

Phase 0 is worth landing on `feat/rdp-live-view` before it merges upstream: three small
changes, no new dependencies, and everything else waits on them.

## 17. Estimates

| Phase | Go | Config/manifests | Notes |
|---|---|---|---|
| 0 | ~150 | — | Plus tests |
| 1 | ~900 | ~120 | `sessiond` wiring dominates |
| 2 | ~1400 | ~200 | Proxy and the Firestore transaction are the hard parts |
| 3 | ~700 | ~150 | Small because §9.1 reuses the MCP reflection |
| 4 | ~1100 | ~80 | Encryption envelope and the lease |
| 5 | ~450 | — | |
| 6 | ~900 | — | CDP extraction plus the cookie-matching table tests |
| 7 | ~600 | ~250 | |

Roughly 6,200 lines of Go. Phase 3 being the smallest is the payoff from `internal/ops`.

## 18. Confidence

| Part | Confidence | Why |
|---|---|---|
| `sessiond` composes the existing pieces | 90 | Each part is proven, but no process runs `api.Server` and `remote.Streamer` together today — `atr remote` attaches to the daemon's Chrome from a *second* process. Collapsing them into one is new wiring, not new mechanism |
| Opal schema generation from `internal/ops` | 93 | The reflection exists and is in use; only `session_id` injection is new |
| Origin and cookie changes unblock the embed | 90 | Mechanism is well understood; CHIPS behaviour across browser versions is the uncertainty |
| Proteus `Bridge` renders the live view | 85 | Spec is clear; not yet tried against a real Opal frontend |
| GKE per-session Pod, isolation model | 88 | Standard patterns; gVisor's effect on Chrome performance is unmeasured |
| Idle timeout and reaping | 92 | Two independent mechanisms, both simple |
| Profile checkpoint and restore (fresh profiles) | 85 | Chrome must be closed cleanly first; corruption is the tail risk |
| **Profile export/import across OSes** | **65** | The CDP approach is sound, but IndexedDB, `HttpOnly`+`Secure` cookie replay, and per-site partitioned storage all have edges we have not probed |
| Warm-pool cold-start target (~2 s) | 70 | Unmeasured. Image pull and gVisor start are the variables |
| Egress cost at 50+ watched sessions | 60 | Estimated from frame size and fps, not measured |

**Overall for phases 0–3: 90. For phases 4–6: 75**, dominated by profile portability.

A spike before committing to phase 6: take a real macOS Chrome profile, export it with the
CDP method, import into a Linux container, and count how many of ten target sites are still
logged in. That number decides whether R2 ships as specified or gets rescoped.

## 19. Open questions

1. **GKE or the VM fallback?** §7.6's `Scheduler` interface keeps the choice reversible, but
   phase 2's shape depends on the answer. Needs a decision before phase 2 starts.
2. **Security review for profile import.** §5.4 scopes by domain and audits origins, but
   copying a live SSO session into shared cloud infrastructure is a policy decision. Phase 6
   is gated on this, not the other way round.
3. **GCS or S3?** Specified GCS, `Store` abstracts it. Is there a multi-cloud commitment that
   makes S3 correct despite the egress and static-credential cost?
4. **Is 20 minutes right?** `session_duration_seconds{reason}` will say. It is per-tenant
   configurable from day one so the answer is cheap to act on.
5. **One session per user, or several?** The spec assumes several, keyed by profile. One is
   simpler and may be enough; it would remove the profile lease entirely.
6. **Does a watching-but-idle viewer keep a session alive?** §4.2 says yes, bounded by a
   4-hour `max_lifetime`. That bound is a guess.
7. **Does `atr computer` come along?** Excluded here. Including it means Xvfb, `CGO_ENABLED=1`,
   a larger image, and a wider syscall surface under gVisor. Recommend browser-only for v1 and
   a separate decision after.
8. **Who owns the broker?** It is Go in this repo but deploys to Cloud Run on Opal's
   infrastructure. If Opal's platform team owns deployment, the repo boundary should probably
   match.
