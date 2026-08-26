package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/imyousuf/agentic-test-runner/internal/agent"
	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/internal/executor"
	"github.com/imyousuf/agentic-test-runner/internal/ops"
	"github.com/imyousuf/agentic-test-runner/internal/secret"
	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// The HUD's LLM client is deliberately long-lived, unlike the per-request
// client in handleAsk. The panel is a running conversation: creating and
// closing a client per turn would drop provider connection state and, for the
// CLI-backed providers, respawn a subprocess on every message.

// handleHudEnable handles POST /api/v1/hud/enable
func (s *Server) handleHudEnable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req ops.HudEnableRequest
	if r.Body != nil {
		// An absent or empty body is fine: every field is optional.
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	if s.appConfig == nil {
		writeError(w, http.StatusInternalServerError, "LLM not configured: app config not provided to server")
		return
	}
	if err := s.appConfig.ValidateForLLM(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("LLM configuration error: %v", err))
		return
	}

	s.hudMu.Lock()
	defer s.hudMu.Unlock()

	res, err := ops.HudEnable(r.Context(), s.browser, s.newHudHandler, req)
	if err != nil {
		// The factory may have already built a client before a later step
		// failed; drop it rather than leaking the process or connection.
		s.closeHudLLM()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleHudDisable handles POST /api/v1/hud/disable
func (s *Server) handleHudDisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	s.hudMu.Lock()
	defer s.hudMu.Unlock()

	res, err := ops.HudDisable(r.Context(), s.browser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.closeHudLLM()
	writeSuccess(w, res)
}

// handleHudStatus handles GET /api/v1/hud/status
func (s *Server) handleHudStatus(w http.ResponseWriter, r *http.Request) {
	res, err := ops.HudStatus(r.Context(), s.browser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

// newHudHandler builds the agent that executes HUD turns. It is the
// ops.HudHandlerFactory the enable handler injects.
//
// The caller must hold s.hudMu.
func (s *Server) newHudHandler(workingDir string) (browser.HudHandler, error) {
	if workingDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolving working directory: %w", err)
		}
		workingDir = cwd
	}

	// Re-enabling with a session already running reuses it, so the
	// conversation survives an `atr browser hud on` issued twice.
	if s.hudSession != nil {
		return s.hudSession.Handler(), nil
	}

	llmCfg := s.appConfig.GetLLMConfig()
	llmClient, err := llm.NewClient(context.Background(), llmCfg)
	if err != nil {
		return nil, fmt.Errorf("creating LLM client: %w", err)
	}

	env := s.appConfig.Executor.Environment
	exec := executor.New(&executor.Config{
		CommandTimeout: s.appConfig.Executor.CommandTimeout,
		MaxOutputSize:  s.appConfig.Executor.MaxOutputSize,
		Environment: executor.EnvironmentConfig{
			AutoDetect:       env.AutoDetect,
			PythonVenvPath:   env.PythonVenvPath,
			CondaEnvName:     env.CondaEnvName,
			NodeVersion:      env.NodeVersion,
			DisablePythonEnv: env.DisablePythonEnv,
			DisableNodeEnv:   env.DisableNodeEnv,
			UseLLMDetection:  env.UseLLMDetection,
		},
	})

	session := agent.NewHudSession(agent.HudConfig{
		LLMClient:  llmClient,
		Browser:    s.browser,
		Vault:      secret.New(s.appConfig.Secrets),
		Executor:   exec,
		WorkingDir: workingDir,
		Verbose:    true,
	})

	s.hudLLM = llmClient
	s.hudSession = session

	return session.Handler(), nil
}

// closeHudLLM tears down the HUD's agent and its LLM client. The caller must
// hold s.hudMu.
func (s *Server) closeHudLLM() {
	if s.hudLLM != nil {
		_ = s.hudLLM.Close()
		s.hudLLM = nil
	}
	s.hudSession = nil
}

// shutdownHud is called from Shutdown so a daemon stop does not leave a CLI
// provider subprocess behind.
func (s *Server) shutdownHud() {
	s.hudMu.Lock()
	defer s.hudMu.Unlock()

	if s.hudSession == nil && s.hudLLM == nil {
		return
	}
	_ = s.browser.DisableHud()
	s.closeHudLLM()
}
