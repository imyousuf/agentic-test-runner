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

ATR supports two LLM backends: **Gemini API** and **Vertex AI**.

### Gemini API

The simplest option for getting started. Uses Google's public Gemini API.

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

# LLM Backend: "gemini-api" or "vertex-ai"
backend: gemini-api

# Model tier: "flash" (faster, cheaper) or "pro" (more capable)
model: flash

# Gemini API settings (when backend: gemini-api)
gemini:
  api_key: ""  # Or use GEMINI_API_KEY env var

# Vertex AI settings (when backend: vertex-ai)
vertex:
  project: ""           # GCP project ID (or GOOGLE_CLOUD_PROJECT env var)
  location: "global"    # GCP region (us-central1, europe-west1, etc.)
  credentials_file: ""  # Path to service account JSON (optional)

# Model name overrides (advanced)
models:
  flash: "gemini-2.0-flash-exp"
  pro: "gemini-2.0-pro-exp"

# Agent settings
agent:
  max_iterations: 100   # Maximum tool calls per analysis
  timeout: "5m"         # Maximum time for analysis
  temperature: 0.3      # LLM temperature (0.0-1.0)

# Command executor settings
executor:
  command_timeout: "2m"      # Timeout for executed commands
  max_output_size: 10485760  # Max output capture (10MB)

# Browser behavior testing settings
behavior:
  base_url: ""  # Default base URL for tests

  browser:
    executable: "auto"       # "auto" to download, or path to browser
    cache_dir: ""            # Browser cache dir (default: ~/.cache/rod)
    headless: true           # Run browser headless
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
```

## Environment Variables

All configuration can be set via environment variables with the `ATR_` prefix:

| Variable | Config Path | Description |
|----------|-------------|-------------|
| `GEMINI_API_KEY` | `gemini.api_key` | Gemini API key |
| `GOOGLE_API_KEY` | `gemini.api_key` | Gemini API key (alternative) |
| `GOOGLE_CLOUD_PROJECT` | `vertex.project` | GCP project for Vertex AI |
| `GOOGLE_CLOUD_LOCATION` | `vertex.location` | GCP region for Vertex AI |
| `GOOGLE_APPLICATION_CREDENTIALS` | `vertex.credentials_file` | Service account key path |
| `ATR_BACKEND` | `backend` | LLM backend |
| `ATR_MODEL` | `model` | Model tier |
| `ATR_AGENT_MAX_ITERATIONS` | `agent.max_iterations` | Max iterations |
| `ATR_AGENT_TIMEOUT` | `agent.timeout` | Agent timeout |

## Example Configurations

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

## Precedence

Configuration is loaded in this order (later overrides earlier):

1. Default values
2. Config file (`~/.atr/config.yaml`)
3. Environment variables
4. Command-line flags

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
