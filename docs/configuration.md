# Configuration

ATR can be configured via a configuration file, environment variables, or command-line flags.

## Configuration File

The default configuration file location is `~/.atr/config.yaml`.

### Create Default Config

```bash
atr config init
```

This creates a config file with default values and helpful comments.

### View Current Config

```bash
atr config show
```

### Validate Config

```bash
atr config validate
```

## LLM Backend Authentication

ATR supports five LLM backends:
- **CLI Backends** (recommended): `claude-cli`, `gemini-cli` - No API keys needed
- **API Backends**: `gemini-api`, `vertex-ai` - Requires API keys or GCP setup
- **Claude on Vertex AI**: `vertex-claude` - Sonnet and Opus over the Messages API, with prompt caching. No API key; uses the same GCP credentials as `vertex-ai`.

### CLI Backends (Recommended)

CLI backends use installed CLI tools (Claude CLI or Gemini CLI) for LLM operations. This is the easiest setup since you don't need separate API keys - the CLI tools handle their own authentication.

#### Auto-Detection

ATR automatically detects installed CLI tools when you run `atr config init`:

```bash
atr config init
# Output: Detected CLI: claude-cli (2.1.3)
#         Using 'claude-cli' as default backend.
```

#### Manual Configuration

```yaml
# ~/.atr/config.yaml
backend: claude-cli  # or gemini-cli

cli:
  auto_detect: true  # Auto-detect available CLIs
  timeout: "5m"      # CLI execution timeout
```

#### CLI Requirements

| Backend | CLI Tool | Installation |
|---------|----------|--------------|
| `claude-cli` | Claude CLI | `npm install -g @anthropic-ai/claude-code` |
| `gemini-cli` | Gemini CLI | `npm install -g @anthropic-ai/gemini-cli` |

#### Check CLI Availability

```bash
atr config show
# Shows: Available CLI Backends:
#          claude-cli (2.1.3)
#          gemini-cli (0.23.0)
```

---

### Gemini API

The simplest option for API-based setup. Uses Google's public Gemini API.

#### Get an API Key

1. Go to [Google AI Studio](https://aistudio.google.com/apikey)
2. Create a new API key
3. Copy the key

#### Configure

**Option 1: Environment Variable (Recommended)**
```bash
export GEMINI_API_KEY="your-api-key"
```

**Option 2: Config File**
```yaml
# ~/.atr/config.yaml
backend: gemini-api
gemini:
  api_key: "your-api-key"
```

**Option 3: Command Line**
```bash
atr run --cmd "go test" --api-key "your-api-key"
```

> **Note**: `GOOGLE_API_KEY` also works as an environment variable.

---

### Vertex AI

For production use, enterprise features, or when you need to keep data within Google Cloud.

#### Prerequisites

1. A Google Cloud project with billing enabled
2. Vertex AI API enabled:
   ```bash
   gcloud services enable aiplatform.googleapis.com
   ```

#### Authentication Methods

Vertex AI supports multiple authentication methods. Choose based on your environment:

##### 1. Application Default Credentials (ADC) - Development

Best for local development. Uses your personal Google account.

```bash
# Login with your Google account
gcloud auth application-default login

# Set your project
export GOOGLE_CLOUD_PROJECT="your-project-id"
```

Config:
```yaml
# ~/.atr/config.yaml
backend: vertex-ai
vertex:
  project: your-project-id
  location: us-central1  # or your preferred region
```

##### 2. Service Account Key File - CI/CD & Servers

Best for automated environments, CI/CD pipelines, and servers.

1. Create a service account:
   ```bash
   gcloud iam service-accounts create atr-sa \
     --display-name="ATR Service Account"
   ```

2. Grant permissions:
   ```bash
   gcloud projects add-iam-policy-binding YOUR_PROJECT \
     --member="serviceAccount:atr-sa@YOUR_PROJECT.iam.gserviceaccount.com" \
     --role="roles/aiplatform.user"
   ```

3. Create and download key:
   ```bash
   gcloud iam service-accounts keys create ~/atr-sa-key.json \
     --iam-account=atr-sa@YOUR_PROJECT.iam.gserviceaccount.com
   ```

4. Configure ATR:

   **Option A: Environment Variable**
   ```bash
   export GOOGLE_APPLICATION_CREDENTIALS="$HOME/atr-sa-key.json"
   export GOOGLE_CLOUD_PROJECT="your-project-id"
   ```

   **Option B: Config File**
   ```yaml
   # ~/.atr/config.yaml
   backend: vertex-ai
   vertex:
     project: your-project-id
     location: us-central1
     credentials_file: /path/to/service-account-key.json
   ```

##### 3. Workload Identity - GKE & Cloud Run

Best for workloads running on Google Cloud (GKE, Cloud Run, Compute Engine).

No key file needed - authentication is automatic via metadata server.

1. Enable Workload Identity on your GKE cluster or use Cloud Run

2. Create a Kubernetes service account and bind it:
   ```bash
   kubectl create serviceaccount atr-ksa

   gcloud iam service-accounts add-iam-policy-binding \
     atr-sa@YOUR_PROJECT.iam.gserviceaccount.com \
     --role roles/iam.workloadIdentityUser \
     --member "serviceAccount:YOUR_PROJECT.svc.id.goog[NAMESPACE/atr-ksa]"

   kubectl annotate serviceaccount atr-ksa \
     iam.gke.io/gcp-service-account=atr-sa@YOUR_PROJECT.iam.gserviceaccount.com
   ```

3. Config (no credentials needed):
   ```yaml
   backend: vertex-ai
   vertex:
     project: your-project-id
     location: us-central1
   ```

##### 4. Compute Engine Default Service Account

For workloads on Compute Engine VMs, the default service account is used automatically.

Ensure the VM's service account has `roles/aiplatform.user` permission.

#### Which Authentication Method to Use?

| Scenario | Recommended Method |
|----------|-------------------|
| Local development | ADC (`gcloud auth application-default login`) |
| CI/CD pipelines | Service Account Key File |
| GitHub Actions | Service Account Key (as secret) |
| GKE | Workload Identity |
| Cloud Run | Workload Identity (automatic) |
| Compute Engine | Default Service Account |
| On-premises server | Service Account Key File |

---

## Complete Configuration Reference

```yaml
# ~/.atr/config.yaml

# LLM Backend: "claude-cli", "gemini-cli", "gemini-api", "vertex-ai",
# or "vertex-claude"
# CLI backends (claude-cli, gemini-cli) don't require API keys
backend: claude-cli

# CLI backend settings (when backend: claude-cli or gemini-cli)
cli:
  auto_detect: true  # Auto-detect available CLIs (default: true)
  timeout: "5m"      # CLI execution timeout (default: 5m)

# Model tier: "flash" (faster, cheaper) or "pro" (more capable)
# For vertex-claude: "sonnet" (faster, cheaper) or "opus" (more capable);
# flash and pro are accepted as aliases of those two.
# Only used for API backends; CLI backends use their own models
model: flash

# Gemini API settings (when backend: gemini-api)
gemini:
  api_key: ""  # Or use GEMINI_API_KEY env var

# Vertex AI settings (when backend: vertex-ai)
vertex:
  project: ""           # GCP project ID (or GOOGLE_CLOUD_PROJECT env var)
  location: "global"    # GCP region (us-central1, europe-west1, etc.)
  credentials_file: ""  # Path to service account JSON (optional)

# Model name overrides (advanced, API backends only)
models:
  flash: "gemini-3.7-flash"
  pro: "gemini-2.0-pro-exp"
  sonnet: "claude-sonnet-5"   # vertex-claude
  opus: "claude-opus-5"       # vertex-claude

# Agent settings
agent:
  max_iterations: 100   # Maximum tool calls per analysis
  timeout: "5m"         # Maximum time for analysis
  temperature: 0.3      # LLM temperature (0.0-1.0)

# Command executor settings
executor:
  command_timeout: "2m"      # Timeout for executed commands
  max_output_size: 10485760  # Max output capture (10MB)
  environment:
    auto_detect: true          # Auto-detect Python/Node.js environments
    use_llm_detection: true    # Use LLM to analyze if command needs environments
    python_venv_path: ""       # Manual path to Python venv
    conda_env_name: ""         # Manual conda environment name
    node_version: ""           # Manual Node.js version for nvm/fnm
    disable_python_env: false  # Disable Python environment detection
    disable_node_env: false    # Disable Node.js environment detection

# Browser behavior testing settings
behavior:
  base_url: ""  # Default base URL for tests

  browser:
    executable: "auto"       # "auto" to download, or path to browser
    version: "latest"        # Browser version: "latest", "stable", "beta", "dev", "canary"
    cache_dir: ""            # Browser cache dir (deprecated, use data_dir)
    data_dir: ""             # Browser data dir for cookies/sessions (default: ~/.atr/browser-data when persist_session is true)
    persist_session: false   # Keep cookies/sessions after browser closes
    headless: true           # Run browser headless
    ignore_https_errors: false  # Ignore SSL certificate errors (for local dev with self-signed certs)
    viewport:
      width: 1920
      height: 1080
    page_timeout: "30s"      # Page load timeout
    action_timeout: "10s"    # Action timeout (click, type, etc.)
    slow_motion: "0s"        # Delay between actions (for debugging)

  capture:
    screenshots: true           # Capture screenshots on failure
    full_page_screenshot: false # Capture full scrollable page
    console_logs: true          # Capture console messages
    network_har: true           # Capture network requests
    dom_snapshot: true          # Capture DOM HTML
    max_console_entries: 100    # Max console entries to capture
    max_network_requests: 50    # Max network requests to capture

# Browser server settings (for `atr browser` commands)
server:
  port: 9333           # HTTP server port
  read_timeout: "30s"  # HTTP read timeout
  write_timeout: "30s" # HTTP write timeout

# Update settings
update:
  auto_update_dev: true  # Auto-update dev versions on startup (every 2 days)
```

## Environment Variables

All configuration can be set via environment variables with the `ATR_` prefix:

| Variable | Config Path | Description |
|----------|-------------|-------------|
| `ATR_BACKEND` | `backend` | LLM backend (`claude-cli`, `gemini-cli`, `gemini-api`, `vertex-ai`, `vertex-claude`) |
| `ATR_CLI_TIMEOUT` | `cli.timeout` | CLI execution timeout |
| `ATR_CLI_AUTO_DETECT` | `cli.auto_detect` | Auto-detect available CLIs |
| `GEMINI_API_KEY` | `gemini.api_key` | Gemini API key |
| `GOOGLE_API_KEY` | `gemini.api_key` | Gemini API key (alternative) |
| `GOOGLE_CLOUD_PROJECT` | `vertex.project` | GCP project for Vertex AI |
| `GOOGLE_CLOUD_LOCATION` | `vertex.location` | GCP region for Vertex AI |
| `GOOGLE_APPLICATION_CREDENTIALS` | `vertex.credentials_file` | Service account key path |
| `ATR_MODEL` | `model` | Model tier (API backends only) |
| `ATR_AGENT_MAX_ITERATIONS` | `agent.max_iterations` | Max iterations |
| `ATR_AGENT_TIMEOUT` | `agent.timeout` | Agent timeout |
| `ATR_BEHAVIOR_BROWSER_DATA_DIR` | `behavior.browser.data_dir` | Browser data directory |
| `ATR_BEHAVIOR_BROWSER_PERSIST_SESSION` | `behavior.browser.persist_session` | Keep sessions after close |
| `ATR_SERVER_PORT` | `server.port` | Browser server port |
| `ATR_UPDATE_AUTO_UPDATE_DEV` | `update.auto_update_dev` | Auto-update dev versions |

## Example Configurations

### CLI Backend (Simplest - No API Key)

```yaml
backend: claude-cli  # or gemini-cli
```

### CLI Backend with Custom Timeout

```yaml
backend: claude-cli
cli:
  timeout: "10m"  # Longer timeout for complex analyses
```

### Minimal Gemini API Setup

```yaml
backend: gemini-api
gemini:
  api_key: "AIza..."
```

### Vertex AI with ADC

```yaml
backend: vertex-ai
vertex:
  project: my-gcp-project
  location: us-central1
```

### Claude on Vertex AI

```yaml
backend: vertex-claude
model: sonnet          # or opus
vertex:
  project: my-gcp-project
  location: global
```

Authenticate the same way as `vertex-ai` — ADC, a service account key, or
workload identity. Claude models are not served from every region; `global`
lets Vertex route the request. Verify with `atr test`.

Each request carries one prompt-cache checkpoint at the end of its fixed
prefix: the tool schemas plus the system prompt, or — for the command-analysis
loop, which has no system prompt — the end of the first message, covering the
instructions and captured failure that stay fixed for the run. Every later
iteration reads that prefix from cache instead of re-sending it.

Two caveats worth knowing when reading token counts:

- Prompts below the API's minimum cacheable size are never cached. Agents with
  small tool sets will show no cache activity at all; this is expected.
- A newly written entry takes a few seconds to become readable, so the first
  iteration or two of a cold run may rewrite it before reads start landing.

`ATR_DEBUG_LLM=1` logs per-request input, output, cache-read and cache-write
token counts.

### Vertex AI with Service Account

```yaml
backend: vertex-ai
vertex:
  project: my-gcp-project
  location: us-central1
  credentials_file: /etc/secrets/gcp-sa.json
```

### Production Setup with Custom Timeouts

```yaml
backend: vertex-ai
model: pro
vertex:
  project: production-project
  location: us-central1

agent:
  max_iterations: 50
  timeout: "10m"
  temperature: 0.2

executor:
  command_timeout: "5m"

behavior:
  browser:
    headless: true
    viewport:
      width: 1920
      height: 1080
  capture:
    screenshots: true
    console_logs: true
```

### Development Setup (Non-headless Browser)

```yaml
backend: gemini-api
gemini:
  api_key: "your-key"

behavior:
  browser:
    headless: false
    slow_motion: "500ms"  # Slow down for debugging
```

### Session Persistence Setup

Keep login sessions across browser restarts:

```yaml
behavior:
  browser:
    persist_session: true
    data_dir: "~/.atr/browser-data"  # Optional, this is the default
```

For project-specific sessions, use `.atr/config.yaml` in your project:

```yaml
behavior:
  browser:
    persist_session: true
    data_dir: ".atr/browser-data"  # Project-local sessions
```

### Local Development with Self-Signed Certificates

When testing against a local server with self-signed SSL certificates:

```yaml
behavior:
  base_url: "https://localhost:3000"
  browser:
    ignore_https_errors: true  # Accept self-signed/invalid certificates
```

### Browser Version Configuration

ATR downloads the latest Chrome for Testing by default. You can configure which version to use:

```yaml
behavior:
  browser:
    version: "latest"   # Always use latest stable (default)
    # version: "beta"   # Use beta channel
    # version: "dev"    # Use dev channel
    # version: "canary" # Use canary channel (bleeding edge)
```

Browsers are cached in `~/.atr/browsers/` by version. Version information is cached for 24 hours in `~/.atr/browser-version.json`.

To use a specific browser installation instead:

```yaml
behavior:
  browser:
    executable: "/path/to/chrome"  # Use this browser instead of downloading
```

### Python/Node.js Environment Detection

ATR can automatically detect and activate Python virtual environments (venv, conda) and Node.js version managers (nvm, fnm) based on the command being run.

```yaml
executor:
  environment:
    auto_detect: true         # Enable auto-detection
    use_llm_detection: true   # Use LLM to analyze commands (recommended)
```

**How it works:**

1. When you run a command like `pytest tests/`, ATR uses an LLM to determine the command needs Python
2. ATR searches for `.venv`, `venv`, or other virtual environments in the working directory
3. Only the Python environment is activated; Node.js environments are skipped

**Manual override example:**

```yaml
executor:
  environment:
    python_venv_path: "/path/to/project/.venv"  # Always use this venv
    node_version: "18"                           # Always use Node 18 via nvm
```

**Disable environment detection:**

```yaml
executor:
  environment:
    auto_detect: false         # Disable all auto-detection
    # Or disable specific environments:
    disable_python_env: true   # Disable Python detection only
    disable_node_env: true     # Disable Node.js detection only
```

Use `atr test-cmd-env "<command>"` to preview which environments would be activated for a command without executing it.

## Precedence

Configuration is loaded in this order (later overrides earlier):

1. Default values
2. User config file (`~/.atr/config.yaml`)
3. Project config file (`.atr/config.yaml` in current directory)
4. Environment variables
5. Command-line flags

### Project-Level Configuration

Create `.atr/config.yaml` in your project directory to override global settings:

```yaml
# .atr/config.yaml (in project root)
behavior:
  browser:
    persist_session: true
    data_dir: ".atr/browser-data"  # Project-local sessions
```

This is useful for:
- Project-specific browser session isolation
- Different base URLs per project
- Custom viewport sizes for specific apps

## Regions for Vertex AI

Vertex AI is available in multiple regions. Choose one close to you:

| Region | Location |
|--------|----------|
| `us-central1` | Iowa, USA |
| `us-east4` | Virginia, USA |
| `us-west1` | Oregon, USA |
| `europe-west1` | Belgium |
| `europe-west4` | Netherlands |
| `asia-northeast1` | Tokyo, Japan |
| `asia-southeast1` | Singapore |

Use `global` for automatic routing (may have higher latency).

## Secret References

Used by the [in-page agent HUD](browser-hud.md) to fill passwords without ever
disclosing them to the model. Each entry maps a name to a command that prints
the secret on stdout; ATR runs the command and types the output straight into
the field.

```yaml
secrets:
  timeout: "60s"                # how long to wait for the password manager
  keep_trailing_newline: false  # keep whitespace the command emits
  refs:
    github/password: "secret-tool lookup service atr account github/password"
    work/vpn: "pass show work/vpn"
    aws/key: "op read op://Private/aws/credential"
```

| Key | Default | Description |
|-----|---------|-------------|
| `secrets.refs` | (none) | Map of name to command |
| `secrets.timeout` | `60s` | Bounds one fetch. Generous by default because managers block on biometric or passphrase prompts |
| `secrets.keep_trailing_newline` | `false` | By default surrounding whitespace is trimmed, since most managers emit a trailing newline that is not part of the secret |

Refs are optional — the agent can also be handed a command directly ("fill the
password by running `pass show github/password`"). Configuring them means the
command never has to be typed into the panel, where it would be visible on
screen.

Any manager that prints to stdout works: `pass`, `secret-tool`, `security`
(macOS Keychain), `op`, `bw`, `gopass`, or your own script.
