# ATR Cloud Sessions — hosted browser sessions for Opal

Status: ideation, for review
Builds on: [`docs/rdp-live-view.md`](./rdp-live-view.md)

## 1. Summary

Run ATR's browser in the cloud, one dedicated Chrome per user, and present its live view
inside Opal through a Proteus `Bridge`. An agent starts a session with a tool call, drives it
with the ordinary ATR primitives, and the person watching can take the keyboard whenever a
step needs a human — a login, an MFA prompt, a CAPTCHA.

```
  agent:  atr_session_start(profile="work")
            → session_id, live view embedded in the chat as a Proteus Bridge
  agent:  atr_navigate(session_id, "https://admin.example.com")
  human:  clicks into the iframe, completes the SSO prompt
  agent:  atr_snapshot(session_id) → continues
  ...20 minutes idle...
  system: profile checkpointed to object storage, Chrome terminated, session EXPIRED
```

Three things make this more than "ATR in a container":

1. **The profile is the durable thing, not the container.** Chrome is disposable; the
   profile lives in object storage, encrypted per user.
2. **The session is the isolation boundary.** One session = one user = one Chrome = one
   profile volume = one network policy. Nothing is shared, ever.
3. **Idle sessions die.** A browser holding a live SSO session is a liability. Twenty
   minutes of no activity and it is checkpointed and destroyed.

## 2. Why

`atr rdp` today assumes the agent and the browser are on the same host, and binds to
loopback. That works for a developer running ATR locally. It does not work for Opal, where
the agent runs in the platform, the user is in a browser tab, and neither is on the machine
that owns Chrome.

Hosting the browser also unlocks the thing the live view was built for. An agent that hits a
login wall on a developer's laptop can ask them to look at `localhost:7788`. An agent running
inside Opal has nowhere to point them. A hosted session with a Proteus `Bridge` puts the
browser directly in the conversation that raised the problem.

## 3. Requirements

| # | Requirement | Where it is addressed |
|---|---|---|
| R1 | Runs on GCP — Cloud Run if it fits, otherwise VMs | §4 |
| R2 | User can upload an existing Chrome profile from their machine | §7.3 |
| R3 | Otherwise ATR creates a fresh profile and maintains it | §7.2 |
| R4 | Profiles persist in an object store between sessions | §7.4 |
| R5 | A loaded session is bound to one user; Chrome is dedicated to them | §8 |
| R6 | No cross-user leak | §8 |
| R7 | Tools that send ATR commands into a session | §9.2 |
| R8 | The live view renders through Proteus UI in Opal | §9.3 |
| R9 | Sessions track activity; 20 minutes idle terminates them | §10 |
| R10 | After expiry the agent must start a new session | §10.4 |

## 4. Where it runs

### 4.1 The shape of the problem

This is a **stateful, addressable, long-lived, single-tenant** workload. A specific request
("click at 400,300 in session `s_abc`") must reach the one process holding that session's
Chrome. That single sentence decides the platform.

### 4.2 Cloud Run

| Property | Cloud Run | Verdict for the data plane |
|---|---|---|
| WebSockets | Supported | ✅ |
| Max request duration | 60 minutes | ⚠️ hard cap on a live-view connection |
| CPU while idle | Available ("CPU always allocated") | ✅ needed, Chrome must not freeze between calls |
| Chrome in the sandbox | Works on gen2 with `--no-sandbox` | ✅ ATR already defaults `--no-sandbox` |
| Xvfb for `atr computer` | Can run in-container | ✅ |
| **Addressing a specific instance** | **Not supported** | ❌ **this is the blocker** |
| Session affinity | Cookie-based, documented best-effort | ❌ not a correctness primitive |
| Instance eviction | Any time, at the platform's discretion | ❌ takes a live session with it |

Cloud Run has no way to say "route this to the instance that owns session `s_abc`". Session
affinity is a performance hint, not a routing guarantee, and the docs are explicit that it
can break. Building per-user dedicated state on a best-effort hint means silent cross-session
misrouting under scale-out — the exact failure R6 forbids.

There is one design that fits inside Cloud Run's model — see §4.5 — and it is worth knowing,
but it does not clear the 60-minute cap.

### 4.3 Recommendation

**Split the plane.**

| Plane | Platform | Why |
|---|---|---|
| Control plane — Opal tool provider, session broker, token minter, reaper | **Cloud Run** | Stateless, request-scoped, scales to zero, cheapest thing to operate |
| Data plane — one Chrome per session | **GKE, one Pod per session** | Addressable Pod IP, no request cap, per-Pod CPU/memory limits, `NetworkPolicy`, clean teardown |

GKE is the recommendation, not a preference for Kubernetes. It is the smallest platform that
gives an addressable, isolated, killable unit of compute with a network policy attached. If
GKE is off the table organisationally, §4.4 is the fallback.

Use **GKE Standard with GKE Sandbox (gVisor)** on the session node pool if the browser will
visit pages outside a trusted allowlist — this workload renders hostile content by
definition. Autopilot is simpler to run but confirm the current state of its sandbox support
before choosing it.

### 4.4 Fallback: a managed instance group of VMs

A regional MIG of Container-Optimized OS VMs, each running a small **node agent** that
accepts "start session" from the broker, launches one session container, and reports
capacity. The broker becomes the scheduler.

Honest accounting: this is re-implementing pod scheduling, bin-packing, health checking,
draining, and rolling upgrades. Choose it only if GKE is genuinely unavailable. Cost is
comparable; operational load is not.

### 4.5 If Cloud Run must host the data plane

The addressability problem is solvable by inverting the connection. The session container
dials **out** to the broker on start and holds a persistent control channel; the broker
routes commands down that channel rather than up to an address.

```
   broker (Cloud Run)                    session instance (Cloud Run)
        │                                          │
        │◀───── register(session_id) ──────────────┤  outbound WS, instance-initiated
        │                                          │
        ├────── command(click 400,300) ───────────▶│
        │◀───── result ────────────────────────────┤
```

This is the ngrok / self-hosted-runner pattern and it genuinely works. What it does not fix:

- The 60-minute request cap still ends the channel. Reconnect is possible, but Chrome's
  in-memory state (open tabs mid-flow, unsaved form input, JS state) does not survive the
  container restart even though the profile does.
- Instance eviction remains uncontrollable.

Acceptable for a v0 demo where sessions are short. Not acceptable as the target.

### 4.6 The fourth option worth taking seriously: bring-your-own runner

The same reverse-tunnel mechanism lets ATR run **on the user's own machine** and register
with the cloud broker. Opal drives it, the live view renders in Opal, but Chrome and the
profile never leave the laptop.

This deserves attention because it dissolves the hardest problems in this document at once:
profile upload becomes unnecessary (§7.3's keychain problem disappears), the compliance
question of copying a corporate SSO session into a shared cloud goes away, and the data-plane
platform question stops mattering. It is not a replacement for hosted sessions — an agent
running overnight needs the cloud — but it is the better default for "use my real browser
identity", and the two modes share almost all their code.

## 5. System architecture

```
   person (Opal chat tab)                        agent (Opal)
          │                                            │
          │  Proteus Bridge iframe                     │  tool call
          ▼                                            ▼
  ┌───────────────────────────────────────────────────────────────────────┐
  │                            Opal platform                              │
  │   chat-frontend ────── TMS (tools-mgmt-service) ────── auth service   │
  │                          │            │                               │
  └──────────────────────────┼────────────┼───────────────────────────────┘
              ui_resource /  │            │  POST /tools/atr_*
              ui_interaction │            │  POST /interactions/execute
                             ▼            ▼
  ┌───────────────────────────────────────────────────────────────────────┐
  │        ATR Session Service — CONTROL PLANE  (Cloud Run, stateless)    │
  │                                                                       │
  │   Opal tool provider      Session broker        Token minter          │
  │   /discovery              create / lookup       session-scoped JWT    │
  │   /tools/{name}           extend / end          5-minute TTL          │
  │   /resources/read         schedule / route                            │
  │   /interactions/execute                                               │
  │                                                                       │
  │   Idle reaper  ◀── Cloud Scheduler, every 60s                         │
  └────────┬──────────────────────┬───────────────────────┬───────────────┘
           │                      │                       │
           ▼                      ▼                       ▼
   ┌───────────────┐    ┌──────────────────┐    ┌────────────────────┐
   │   Firestore   │    │   GKE API        │    │  GCS + Cloud KMS   │
   │ session       │    │ create / delete  │    │  profile store,    │
   │ registry,     │    │ session Pods     │    │  per-tenant CMEK   │
   │ profile lease │    │                  │    │                    │
   └───────────────┘    └────────┬─────────┘    └────────────────────┘
                                 │
   ══════════════════════════════╪══════════════════ trust boundary ═════
                                 ▼
  ┌───────────────────────────────────────────────────────────────────────┐
  │                DATA PLANE — one Pod per session (GKE)                 │
  │                                                                       │
  │   ┌─────────────────────────────────────────────────────────────┐    │
  │   │  Pod  session=s_abc  owner=u_123     gVisor, non-root       │    │
  │   │                                                             │    │
  │   │   init: fetch + decrypt profile from GCS ──▶ /profile       │    │
  │   │                                                             │    │
  │   │   atr sessiond                                              │    │
  │   │     ├── REST daemon      (internal/api)   ops.* primitives  │    │
  │   │     ├── live view        (internal/rdp)   /ws, frames+input │    │
  │   │     ├── idle watchdog    self-terminate at 20 min           │    │
  │   │     └── checkpointer     periodic + on SIGTERM              │    │
  │   │                    │                                        │    │
  │   │                    ▼  CDP                                   │    │
  │   │            Chrome  --user-data-dir=/profile                 │    │
  │   │                                                             │    │
  │   │   emptyDir /profile — never shared, destroyed with the Pod  │    │
  │   └─────────────────────────────────────────────────────────────┘    │
  │                                                                       │
  │   NetworkPolicy: egress → internet OK                                 │
  │                  egress → VPC internals, metadata server DENIED       │
  └───────────────────────────────────────────────────────────────────────┘
```

Note what is *not* in the data plane: no LLM keys, no cross-session cache, no shared volume,
no access to the control plane's database. A compromised session Pod can reach the public
internet and nothing else.

## 6. Session lifecycle

### 6.1 States

```
                    atr_session_start
                           │
                           ▼
                    ┌─────────────┐   no capacity / profile locked
                    │  PENDING    │──────────────────────────────▶ FAILED
                    └──────┬──────┘
                           │ Pod scheduled, profile restored, Chrome up
                           ▼
                    ┌─────────────┐◀────── any activity resets the idle clock
                    │   READY     │
                    └──┬───┬───┬──┘
        17 min idle    │   │   │   atr_session_end / user clicks "End"
                       │   │   └──────────────────────────┐
                       │   │ 20 min idle                  │
                       ▼   ▼                              ▼
                 ┌──────────────┐                  ┌─────────────┐
                 │   EXPIRING   │─── checkpoint ──▶│  STOPPING   │
                 │  warning     │                  │  checkpoint │
                 │  pushed to   │                  │  → GCS      │
                 │  UI + agent  │                  └──────┬──────┘
                 └──────────────┘                         │
                       │ activity                         ▼
                       └──────▶ READY               ┌─────────────┐
                                                    │  TERMINATED │
                                                    └─────────────┘
```

`EXPIRING` is not decoration. Three minutes of warning is the difference between "the agent
saw it coming and wrapped up" and "the agent's next tool call failed for no visible reason".

### 6.2 Start sequence

```
  agent        TMS        session svc      Firestore     GKE        GCS      Pod
    │           │              │               │          │          │        │
    ├─ tool ───▶│              │               │          │          │        │
    │  start    ├─ /tools/ ───▶│               │          │          │        │
    │           │  atr_session │ resolve user  │          │          │        │
    │           │  _start      │ from auth ctx │          │          │        │
    │           │              ├─ lease ──────▶│          │          │        │
    │           │              │  profile      │  ok      │          │        │
    │           │              │◀──────────────┤          │          │        │
    │           │              ├─ create Pod ─────────────▶│          │        │
    │           │              │                          ├─ schedule ───────▶│
    │           │              │                          │          │◀─ get ─┤
    │           │              │                          │          ├─ tar ─▶│
    │           │              │                          │          │  decrypt
    │           │              │◀──────── ready ──────────────────────────────┤
    │           │              │ mint session JWT (5 min) │          │        │
    │           │◀─ Proteus ───┤                          │          │        │
    │           │   Document   │                          │          │        │
    │◀─ result ─┤   + Bridge   │                          │          │        │
    │  session_id                                                             │
```

Cold start is dominated by profile restore. Budget: Pod schedule 2–5 s (pre-warmed node pool),
image pull 0 s (pre-pulled), profile fetch+decompress 1–15 s depending on size, Chrome launch
1–2 s. **Keep a warm pool of 2–5 blank Pods** so a fresh-profile session starts in ~2 s and
only profile restore pays the tail.

## 7. Profiles

### 7.1 What a profile actually is

A Chrome `--user-data-dir` is a directory tree, and most of it is disposable. What matters:

| Keep | Why |
|---|---|
| `Default/Cookies` | Session cookies — the whole point |
| `Default/Login Data` | Saved passwords |
| `Default/Web Data` | Autofill |
| `Default/Preferences` | Per-profile settings |
| `Local State` | **Holds the encryption key for the above** |
| `Default/Local Storage`, `Default/IndexedDB` | Modern apps keep auth state here |
| `Default/Extensions`, `Default/Secure Preferences` | Only if extensions are in scope |

| Drop | Why |
|---|---|
| `Default/Cache`, `Code Cache`, `GPUCache` | Regenerated; often 90% of the bytes |
| `Default/Service Worker/CacheStorage` | Regenerated |
| `Default/History`, `Top Sites`, `Favicons` | Not needed, and privacy-sensitive |
| `Crashpad`, `*.log`, lock files | Noise, and stale locks break startup |

Stripping caches typically takes a 1–3 GB profile down to 5–50 MB. Do it before anything
touches the network.

### 7.2 Fresh profile (the default)

ATR creates a blank `user-data-dir`, the user logs into whatever they need **through the live
view**, and the profile is checkpointed from then on. This is the recommended default: no
upload, no cryptography problem, no compliance question, and it is exactly the interaction the
live view was built for.

### 7.3 Uploading an existing profile — and the problem with it

> **This is the hardest requirement in the document, and the naïve version does not work.**

Chrome encrypts `Cookies` and `Login Data` with a key held by the **operating system's**
credential store:

| Source OS | Key custody | Copied to a Linux container? |
|---|---|---|
| macOS | Keychain, `Chrome Safe Storage` | ❌ undecryptable |
| Windows | DPAPI, bound to the user + machine | ❌ undecryptable |
| Linux | gnome-keyring / kwallet, or the hardcoded `peanuts` fallback | ⚠️ only if the source used the fallback |

So "zip up `~/Library/Application Support/Google/Chrome` and upload it" produces a profile
that starts fine and is logged into nothing. Bookmarks survive. Sessions do not.

**Recommended path — export the state, not the directory.** A client-side command extracts
decrypted state through the user's *own* Chrome (which can talk to its own keychain), and the
cloud session injects it via CDP into a fresh profile:

```
  user's machine                                     cloud session Pod
  ──────────────                                     ─────────────────
  atr profile export --domains example.com,corp.net
        │
        ├─ launch/attach to the user's Chrome over CDP
        ├─ Network.getAllCookies          (Chrome decrypts, we never touch the key)
        ├─ Storage.getStorageKey… localStorage / IndexedDB for the chosen origins
        ├─ filter to the requested domains          ◀── scope, do not vacuum everything
        └─ age/AES-GCM encrypt → atr-profile.bundle
                                │
                                │  upload, client-side encrypted
                                ▼
                        GCS  ──────────────────────▶  init container decrypts
                                                      Network.setCookies
                                                      Storage.setLocalStorage…
                                                      → fresh Linux profile, logged in
```

Advantages beyond correctness: it is **scoped** (the user names the domains rather than
shipping every session they hold), it is **auditable** (the bundle manifest lists exactly
which origins moved), and it is **portable** across OSes because it never carries an
OS-bound key.

Still offer raw-directory upload for the Linux-to-Linux case and for bookmarks/extensions,
but label it clearly: *settings and bookmarks transfer; logins usually do not.*

**Flag for a decision before building any of this:** importing a user's live SSO sessions
into shared cloud infrastructure is a security and compliance decision, not an engineering
one. It means a cloud process can act as that user against every imported domain. Domain
scoping, an explicit consent step naming the domains, per-session audit logging, and a short
default profile retention are the minimum. §4.6's BYO-runner mode avoids the question
entirely and may be the right answer for regulated tenants.

### 7.4 Storage layout

The requirement says S3. On GCP, **use GCS** — same object semantics, no cross-cloud egress
charge, native CMEK through Cloud KMS, and Workload Identity instead of long-lived access
keys. Put a `ProfileStore` interface in front of it so S3 remains a drop-in if multi-cloud
becomes real.

```
  gs://atr-profiles-{env}/
    {tenant_id}/
      {user_id_hash}/
        {profile_id}/
          manifest.json          version, origins, size, checksum, chrome version
          profile-{ts}.tar.zst   encrypted with a per-user DEK
          profile-{ts-1}.tar.zst previous checkpoint, kept for rollback
```

- **Encryption**: per-user DEK, wrapped by a per-tenant KEK in Cloud KMS. A bucket-policy
  mistake then leaks ciphertext only, and a tenant's keys are independently revocable.
- **Retention**: lifecycle rule deletes checkpoints after N days idle. Two generations kept
  so a corrupt checkpoint is recoverable.
- **Locking**: Chrome corrupts a `user-data-dir` shared by two processes. Firestore lease on
  `{tenant}/{user}/{profile_id}` with a TTL, renewed by the session; a second start on the
  same profile is refused with a structured error naming the live session.
- **Checkpointing**: on graceful stop, on the idle reaper's kill, and every 5 minutes while
  active. Chrome must be closed or at least quiesced before the tar, or the SQLite files are
  captured mid-write.

## 8. Isolation — how "no cross-user leak" is actually enforced

Defence at four layers, because any one of them will eventually have a bug.

```
  ┌─ 1. Identity ───────────────────────────────────────────────────────┐
  │  Opal auth context → user_id. The broker binds session→owner at     │
  │  creation. Every subsequent call re-checks owner == caller.         │
  │  A session id is never a capability on its own.                     │
  └─────────────────────────────────────────────────────────────────────┘
  ┌─ 2. Token ──────────────────────────────────────────────────────────┐
  │  Live view and command traffic carry a session-scoped JWT:          │
  │    { sub: user_id, sid: session_id, aud: pod, exp: now+5m }         │
  │  Signed by the broker, verified in the Pod. Short TTL so a leaked   │
  │  iframe URL in a screenshot or a log is worthless in minutes.       │
  └─────────────────────────────────────────────────────────────────────┘
  ┌─ 3. Compute ────────────────────────────────────────────────────────┐
  │  One Pod, one Chrome, one user. Profile on an emptyDir that dies    │
  │  with the Pod. Non-root, read-only rootfs, seccomp, gVisor.         │
  │  No shared tmp, no shared cache, no sidecar reused across sessions. │
  └─────────────────────────────────────────────────────────────────────┘
  ┌─ 4. Network ────────────────────────────────────────────────────────┐
  │  NetworkPolicy: egress to the internet, DENY to RFC1918 and to      │
  │  169.254.169.254. Without the metadata-server rule a hostile page   │
  │  can mint a node service-account token — the whole model falls.     │
  └─────────────────────────────────────────────────────────────────────┘
```

The single most important line above is the metadata-server deny. A browser session is, by
construction, code execution from untrusted parties inside your VPC.

Audit every command with `(user_id, session_id, tool, timestamp, target_origin)`. When
someone asks "what did the agent do with my Salesforce session", the answer must exist.

## 9. Opal integration

### 9.1 Where the pieces plug in

Opal's TMS already has the two seams needed, so no platform changes are required:

- `ui_resource` — a tool declares `ui://atr/live-view`; TMS fetches it from our
  `/resources/read` and hands the frontend a Proteus document to render.
- `ui_interaction` — the rendered UI posts back through TMS to our
  `/interactions/execute`, routed by `(name, parameters)`.

### 9.2 Tools

Do **not** ship a generic `atr_exec(session_id, command)`. It is a remote shell wearing a tool
schema, it is unreviewable, and it defeats the audit story in §8.

Instead, generate the tool surface from `internal/ops`. That layer already carries JSON tags
and `jsonschema:"required"` / `jsonschema_description:"..."` annotations, and already drives
both REST decoding and MCP `inputSchema` reflection — the same reflection produces the Opal
discovery manifest. One source of truth, four surfaces.

```
  internal/ops   ── ops.ClickRequest, ops.NavigateRequest, ... (+ schema tags)
       │
       ├──▶ REST      internal/api      (exists)
       ├──▶ MCP       internal/mcp      (exists)
       ├──▶ CLI       internal/cli      (exists)
       └──▶ Opal      internal/opal     (new — same reflection, adds session_id)
```

| Tool | Purpose |
|---|---|
| `atr_session_start(profile?, purpose?)` | Create or resume; returns `session_id` + a Proteus document with the live view |
| `atr_session_status(session_id)` | State, idle remaining, current URL, viewer count |
| `atr_session_extend(session_id)` | Reset the idle clock — for a long unattended step |
| `atr_session_end(session_id)` | Checkpoint and terminate |
| `atr_navigate`, `atr_click`, `atr_fill`, `atr_snapshot`, `atr_screenshot`, `atr_eval`, … | Reflected from `internal/ops`, each taking `session_id` |
| `atr_profile_list()` / `atr_profile_import()` | Manage stored profiles |

Every session-scoped tool resolves `session_id` → owner and rejects a mismatch with an RFC
9457 `ProblemDetail`, which the Opal SDK already models:

```json
{ "type": "https://atr.dev/errors/session-expired",
  "title": "Session expired",
  "status": 410,
  "error_code": "session_expired",
  "detail": "Session s_abc was terminated after 20 minutes idle. Call atr_session_start." }
```

### 9.3 Rendering the live view — `Bridge`, not `Federated`

Proteus offers two escape hatches from its component set:

| | `ProteusBridge` | `ProteusFederated` |
|---|---|---|
| Mechanism | iframe on a `ui://` resource | Module Federation `remoteEntry.js` into the host |
| Isolation | Separate origin, separate JS context | Runs inside Opal's page |
| Existing code reuse | The branch's `web/` SPA runs as-is | Must be rebuilt as a federated remote with shared deps |
| Degradation | Documented `fallback` for Slack, Teams, mobile | None |

**Use `Bridge`.** The live view is a self-contained canvas with its own WebSocket; it gains
nothing from sharing Opal's React tree and it carries untrusted rendered content, which is
precisely what an iframe boundary is for. The `web/` application on `feat/rdp-live-view` is
already the right artifact.

```json
{
  "$type": "Document",
  "appName": "ATR",
  "title": "Browser session",
  "subtitle": "Chrome 141 · profile \"work\" · 18 min remaining",
  "body": [
    { "$type": "Bridge", "resource": "ui://atr/live-view", "height": 720,
      "fallback": { "$type": "Alert",
                    "children": "Open this conversation in Opal web to view the browser." } },
    { "$type": "Group", "flexDirection": "row", "gap": "8", "children": [
      { "$type": "Badge", "children": "READY" },
      { "$type": "Text",  "children": "https://admin.example.com/users" } ] }
  ],
  "actions": [
    { "$type": "Action", "appearance": "primary", "children": "Take over",
      "onClick": { "interaction": "atr_takeover", "params": { "session_id": "s_abc" } } },
    { "$type": "Action", "children": "Extend 20 min",
      "onClick": { "interaction": "atr_extend",   "params": { "session_id": "s_abc" } } },
    { "$type": "Action", "appearance": "danger", "children": "End session",
      "onClick": { "interaction": "atr_end",      "params": { "session_id": "s_abc" } } }
  ]
}
```

"Take over" matters for more than UX. While a human holds input, the agent's tool calls
should queue or be rejected — two actors driving one Chrome produces races that look like
flaky tests. The `--view-only` flag already on the branch is the server-side half of this;
takeover flips it per-session.

### 9.4 Two concrete blockers in the current RDP code

Both are small, both will otherwise fail on first contact with an Opal iframe.

**Origin check.** `internal/rdp/server.go:checkOrigin` accepts only loopback origins:

```go
return host == "127.0.0.1" || host == "localhost" || host == "[::1]" || host == "::1"
```

An Opal-hosted iframe fails this and the WebSocket upgrade is refused. Needs a configurable
allowlist (`--allow-origin`, repeatable) defaulting to today's loopback set.

**Cookie `SameSite`.** The auth cookie is set `SameSite=Strict`:

```go
http.SetCookie(w, &http.Cookie{ Name: cookieName, ..., SameSite: http.SameSiteStrictMode })
```

A `Strict` cookie is not sent on requests from a cross-origin iframe, so the SPA loads and
then every asset and the WebSocket 401. Embedded mode needs
`SameSite=None; Secure; Partitioned` (CHIPS), keeping `Strict` for the loopback default.

## 10. Idle timeout

### 10.1 What counts as activity

| Signal | Counts? | Reasoning |
|---|---|---|
| Any `atr_*` tool call on the session | Yes | The agent is working |
| Live-view input — mouse, key, navigate | Yes | A person is working |
| Live-view WebSocket attached, no input | Yes, but see below | Someone is watching |
| Frames flowing from a self-refreshing page | **No** | A dashboard on a 5-second refresh would keep a session alive forever |

An attached-but-silent viewer counting as activity means a forgotten browser tab pins a
session indefinitely. Bound it with an **absolute session lifetime** (4 hours default),
enforced regardless of activity.

### 10.2 Two watchdogs, deliberately

```
  in-Pod watchdog                        broker reaper (Cloud Scheduler → 60s)
  ───────────────                        ────────────────────────────────────
  lastActivity in memory                 Firestore: session.expires_at
  tick 10s                               query where expires_at < now
  ├─ 17 min → push sessionExpiring       ├─ delete the Pod
  │           to viewers + mark the      ├─ release the profile lease
  │           state so tools warn        └─ mark TERMINATED
  └─ 20 min → checkpoint, SIGTERM self

  heartbeat every 30s ──────────────────▶ renews expires_at
```

The in-Pod watchdog is fast and can checkpoint cleanly. It is also useless if the process
wedges — which is exactly when you most want the session gone. The broker reaper is the
authority: no heartbeat, no session, regardless of what the Pod believes.

### 10.3 Shutdown must be graceful

`terminationGracePeriodSeconds: 60`. On `SIGTERM`: stop accepting input → close Chrome
cleanly so SQLite settles → tar+zstd → encrypt → upload → exit. A `SIGKILL` mid-write is how
profiles get corrupted, which is why §7.4 keeps the previous checkpoint.

### 10.4 What the agent sees

R10 says the agent must start a new session after expiry. Make that unmissable:

- At 17 minutes, successful tool results carry
  `warning: "session s_abc expires in 3 minutes; call atr_session_extend to keep it"`.
- After expiry, every session-scoped tool returns the `session_expired` `ProblemDetail` from
  §9.2 — status 410, an `error_code` the agent can branch on, and a `detail` that names the
  remedy. Do not return a generic 404; an agent that cannot distinguish "wrong id" from
  "timed out" will retry the wrong thing.

## 11. Changes needed in ATR

| # | Change | Package | Size |
|---|---|---|---|
| 1 | Configurable WebSocket origin allowlist | `internal/rdp` | S |
| 2 | `SameSite=None; Secure; Partitioned` in embedded mode | `internal/rdp` | S |
| 3 | `atr sessiond` — one process: REST daemon + live view + watchdog + checkpointer | `internal/cli`, new `internal/session` | L |
| 4 | Profile store abstraction — GCS and S3 backends, tar+zstd, KMS envelope encryption | new `internal/profile` | L |
| 5 | `atr profile export` — client-side CDP cookie/storage extraction, domain-scoped | new `internal/profile` | M |
| 6 | `atr profile import` — inject a bundle into a fresh profile over CDP | new `internal/profile` | M |
| 7 | Opal tool provider — discovery, tools, resources, interactions, reflected from `internal/ops` | new `internal/opal` | M |
| 8 | Per-session takeover — flip `view-only` at runtime, gate agent input while held | `internal/rdp` | M |
| 9 | Health and readiness endpoints for k8s probes | `internal/api` | S |
| 10 | Container image — Chrome, fonts, optional Xvfb, non-root | `Dockerfile` | M |

Items 1, 2, and 9 are worth doing on `feat/rdp-live-view` before it merges; they are small and
they unblock everything else.

## 12. Scale and cost

Per session, measured from the branch's defaults (20 fps, quality 60, max width 1600):

| Resource | Estimate | Note |
|---|---|---|
| CPU | 0.5–1.5 vCPU | JPEG encoding dominates during interaction; near zero when the page is still |
| Memory | 1.5–2 GiB | Chrome plus the Go process |
| Egress per viewer | ~0.8 MB/s ≈ 6.4 Mbps | 20 fps × ~40 KB frames |

Egress is the surprise. A hundred concurrent viewed sessions is ~640 Mbps sustained, and
cloud egress is billed. Mitigations, roughly in order of value:

1. **Adaptive quality** — the branch already anticipates this. Drop fps and quality when the
   page is static or the link is slow.
2. **Stream only when watched.** An agent-only session needs no frames at all; screencast
   should start on the first viewer and stop on the last. This alone removes most of the
   cost, because most sessions are never watched.
3. **WebRTC / VP8** later. A 5–10× bandwidth win over JPEG, at a real complexity cost. Not
   for v1.

Scale-to-zero on the data plane is genuine: no sessions, no Pods, and the control plane on
Cloud Run costs nothing idle. The warm pool from §6.2 is the only standing cost.

## 13. Phases

| Phase | Content | Result |
|---|---|---|
| 0 | RDP items 1, 2, 9 from §11 | The existing live view can be embedded cross-origin |
| 1 | `atr sessiond` container, one Pod, manual start, fresh profile only | A hosted session, driven by REST |
| 2 | Session broker on Cloud Run, Firestore registry, JWTs, GKE scheduling | Multi-user, isolated, addressable |
| 3 | Opal tool provider + Proteus `Bridge` + interactions | The agent starts a session; the person sees it in the chat |
| 4 | Profile store: fresh-profile checkpoint and restore | Sessions remember their logins |
| 5 | Idle watchdog, reaper, expiry warnings, `session_expired` | R9 and R10 |
| 6 | `atr profile export` / `import`, domain-scoped | R2, the version that actually works |
| 7 | Adaptive quality, stream-only-when-watched, gVisor, NetworkPolicy hardening | Production |

Phases 0–3 are the demo. Phases 4–5 are what make it a product. Phase 6 is the one to
schedule after the security review in §7.3, not before.

## 14. Risks

| Risk | Severity | Handling |
|---|---|---|
| Uploaded profiles do not decrypt off their origin OS | High — silently breaks R2 | §7.3 export-the-state approach; set expectations early |
| Importing live SSO sessions into shared cloud | High — compliance | Domain scoping, explicit consent, audit, short retention; offer §4.6 BYO runner |
| Cloud Run chosen for the data plane on affinity | High — silent cross-session misrouting | §4.2; do not build on best-effort affinity |
| Metadata-server access from a hostile page | High | NetworkPolicy deny, enforced in CI on the manifest |
| Profile corruption on ungraceful kill | Medium | Two checkpoint generations, grace period, quiesce before tar |
| Egress cost at scale | Medium | Stream only when watched; adaptive quality |
| Cold start on a large profile | Medium | Cache-stripping, zstd, warm pool, background prefetch on likely-next profile |
| Agent and human driving simultaneously | Medium — looks like flakiness | Explicit takeover; gate agent input while held |
| 20 min is wrong for real workflows | Low | Make it policy-configurable per tenant; `atr_session_extend` exists |

## 15. Open questions

1. **GKE or the VM fallback?** §4.3 recommends GKE. If the org has no GKE footprint the
   answer changes the shape of phase 2 substantially.
2. **Is profile upload in scope for v1 at all**, given §7.3? Fresh-profile-plus-live-view-login
   covers most real use and carries none of the risk.
3. **GCS or S3?** The requirement says S3; GCP says GCS. Is there an existing multi-cloud
   commitment that makes S3 correct despite the egress and credential cost?
4. **Does a watching-but-idle viewer keep a session alive?** §10.1 says yes with a 4-hour
   absolute cap. That cap is a guess and should come from how people actually use it.
5. **One session per user, or several?** The document assumes several, keyed by profile. One
   is simpler and may be enough.
6. **Who pays for an abandoned session** — is there a per-tenant concurrent-session quota,
   and what happens at the limit?
7. **Does `atr computer` (desktop control) come along?** It needs Xvfb in the image and
   changes the isolation story. Browser-only is the smaller, safer v1.
