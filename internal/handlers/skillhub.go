package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"ClawDeckX/internal/logger"
	"ClawDeckX/internal/web"
)

// SkillHubHandler handles SkillHub-related operations
type SkillHubHandler struct {
	// Server-side cache for proxied SkillHub data (avoids re-fetching 3-5MB JSON from CDN)
	cacheMu   sync.Mutex
	cacheData json.RawMessage
	cacheURL  string
	cacheTime time.Time
	cacheTTL  time.Duration
	gwClient  GatewayClient
}

// GatewayClient interface for OpenClaw Gateway RPC calls
type GatewayClient interface {
	Request(method string, params interface{}) (json.RawMessage, error)
}

// NewSkillHubHandler creates a new SkillHub handler
func NewSkillHubHandler() *SkillHubHandler {
	return &SkillHubHandler{
		cacheTTL: 1 * time.Hour,
	}
}

// SetGatewayClient sets the Gateway client for RPC calls
func (h *SkillHubHandler) SetGatewayClient(client GatewayClient) {
	h.gwClient = client
}

// CLIStatus checks if SkillHub CLI is installed
// GET /api/v1/skillhub/cli-status
func (h *SkillHubHandler) CLIStatus(w http.ResponseWriter, r *http.Request) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd.exe", "/c", "skillhub", "--version")
	} else {
		cmd = exec.Command("skillhub", "--version")
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// CLI not installed or not in PATH
		web.OK(w, r, map[string]interface{}{
			"installed": false,
			"version":   nil,
			"path":      nil,
		})
		return
	}

	version := strings.TrimSpace(stdout.String())
	if version == "" {
		version = strings.TrimSpace(stderr.String())
	}

	// Try to get the path
	var pathCmd *exec.Cmd
	if runtime.GOOS == "windows" {
		pathCmd = exec.Command("cmd.exe", "/c", "where", "skillhub")
	} else {
		pathCmd = exec.Command("which", "skillhub")
	}

	var pathOut bytes.Buffer
	pathCmd.Stdout = &pathOut
	pathCmd.Run()

	cliPath := strings.TrimSpace(pathOut.String())

	web.OK(w, r, map[string]interface{}{
		"installed": true,
		"version":   version,
		"path":      cliPath,
	})
}

// Install installs SkillHub CLI
// POST /api/v1/skillhub/install
func (h *SkillHubHandler) Install(w http.ResponseWriter, r *http.Request) {
	if runtime.GOOS == "windows" {
		web.Fail(w, r, "PLATFORM_NOT_SUPPORTED", "One-click install is not supported on Windows. Please install manually.", http.StatusBadRequest)
		return
	}

	logger.Log.Info().Msg("starting SkillHub CLI installation")

	// Create install script
	installScript := `#!/usr/bin/env bash
set -euo pipefail

KIT_URL="https://skillhub-1251783334.cos.ap-guangzhou.myqcloud.com/install/latest.tar.gz"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

echo "Downloading SkillHub CLI..."
curl -fsSL "$KIT_URL" -o "$TMP_DIR/latest.tar.gz"

echo "Extracting..."
tar -xzf "$TMP_DIR/latest.tar.gz" -C "$TMP_DIR"

INSTALLER="$TMP_DIR/cli/install.sh"
if [[ ! -f "$INSTALLER" ]]; then
  echo "Error: install.sh not found at $INSTALLER" >&2
  find "$TMP_DIR" -maxdepth 3 -print >&2
  exit 1
fi

echo "Running installer..."
bash "$INSTALLER" "$@"
`

	// Save script to temp file
	tmpDir := os.TempDir()
	scriptPath := filepath.Join(tmpDir, "skillhub-install.sh")
	err := os.WriteFile(scriptPath, []byte(installScript), 0755)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to create install script")
		web.Fail(w, r, "SCRIPT_CREATE_FAILED", err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.Remove(scriptPath)

	// Execute install script
	cmd := exec.Command("bash", scriptPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	select {
	case err := <-done:
		output := stdout.String()
		errOutput := stderr.String()

		if err != nil {
			logger.Log.Error().Err(err).Str("stdout", output).Str("stderr", errOutput).Msg("SkillHub installation failed")

			// Check for permission errors
			if strings.Contains(errOutput, "Permission denied") || strings.Contains(output, "Permission denied") {
				web.Fail(w, r, "PERMISSION_DENIED", "Permission denied. Please run with sudo or as administrator.", http.StatusForbidden)
				return
			}

			web.Fail(w, r, "INSTALL_FAILED", errOutput+"\n"+output, http.StatusInternalServerError)
			return
		}

		logger.Log.Info().Str("output", output).Msg("SkillHub CLI installed successfully")
		web.OK(w, r, map[string]interface{}{
			"success": true,
			"output":  output,
		})

	case <-time.After(5 * time.Minute):
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		web.Fail(w, r, "INSTALL_TIMEOUT", "Installation timed out after 5 minutes", http.StatusGatewayTimeout)
	}
}

// InstallSkill installs a specific skill using SkillHub CLI
// POST /api/v1/skillhub/install-skill
func (h *SkillHubHandler) InstallSkill(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.Fail(w, r, "INVALID_REQUEST", "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Slug == "" {
		web.Fail(w, r, "INVALID_REQUEST", "skill slug is required", http.StatusBadRequest)
		return
	}

	// Validate slug: only allow alphanumeric, hyphens, underscores, dots (prevent injection)
	for _, c := range req.Slug {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			web.Fail(w, r, "INVALID_PARAM", "invalid slug characters", http.StatusBadRequest)
			return
		}
	}

	logger.Log.Info().Str("slug", req.Slug).Msg("installing skill via SkillHub CLI")

	// Check if SkillHub CLI is installed
	var checkCmd *exec.Cmd
	if runtime.GOOS == "windows" {
		checkCmd = exec.Command("cmd.exe", "/c", "skillhub", "--version")
	} else {
		checkCmd = exec.Command("skillhub", "--version")
	}

	if err := checkCmd.Run(); err != nil {
		web.Fail(w, r, "CLI_NOT_INSTALLED", "SkillHub CLI is not installed", http.StatusBadRequest)
		return
	}

	// Execute skillhub install command
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd.exe", "/c", "skillhub", "install", req.Slug)
	} else {
		cmd = exec.Command("skillhub", "install", req.Slug)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	select {
	case err := <-done:
		output := stdout.String()
		errOutput := stderr.String()

		if err != nil {
			logger.Log.Error().Err(err).Str("slug", req.Slug).Str("stdout", output).Str("stderr", errOutput).Msg("skill installation failed")
			web.Fail(w, r, "INSTALL_FAILED", errOutput+"\n"+output, http.StatusInternalServerError)
			return
		}

		logger.Log.Info().Str("slug", req.Slug).Str("output", output).Msg("skill installed successfully")
		web.OK(w, r, map[string]interface{}{
			"success": true,
			"output":  output,
			"slug":    req.Slug,
		})

	case <-time.After(3 * time.Minute):
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		web.Fail(w, r, "INSTALL_TIMEOUT", "Installation timed out after 3 minutes", http.StatusGatewayTimeout)
	}
}

// ListInstalledSkills returns the list of skills installed via SkillHub CLI.
// GET /api/v1/skillhub/installed-skills
func (h *SkillHubHandler) ListInstalledSkills(w http.ResponseWriter, r *http.Request) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd.exe", "/c", "skillhub", "list")
	} else {
		cmd = exec.Command("skillhub", "list")
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	select {
	case err := <-done:
		if err != nil {
			// CLI not installed or command failed
			web.OK(w, r, map[string]interface{}{
				"skills":    []interface{}{},
				"available": false,
			})
			return
		}

		// Parse output: each line is "slug  version"
		lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
		type installedSkill struct {
			Slug    string `json:"slug"`
			Version string `json:"version"`
		}
		var skills []installedSkill
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				skills = append(skills, installedSkill{Slug: fields[0], Version: fields[1]})
			} else if len(fields) == 1 {
				skills = append(skills, installedSkill{Slug: fields[0]})
			}
		}

		web.OK(w, r, map[string]interface{}{
			"skills":    skills,
			"available": true,
		})

	case <-time.After(15 * time.Second):
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		web.OK(w, r, map[string]interface{}{
			"skills":    []interface{}{},
			"available": false,
		})
	}
}

// ProxyData proxies the SkillHub JSON data with server-side caching.
// The upstream JSON is ~3-5MB; without caching every page visit re-downloads it.
// GET /api/v1/skillhub/data?url=<encoded_url>
func (h *SkillHubHandler) ProxyData(w http.ResponseWriter, r *http.Request) {
	dataURL := r.URL.Query().Get("url")
	if dataURL == "" {
		dataURL = "https://cloudcache.tencentcs.com/qcloud/tea/app/data/skills.33d56946.json"
	}

	// Check server-side cache (same URL + within TTL)
	h.cacheMu.Lock()
	if h.cacheData != nil && h.cacheURL == dataURL && time.Since(h.cacheTime) < h.cacheTTL {
		cached := h.cacheData
		h.cacheMu.Unlock()
		logger.Log.Debug().Str("url", dataURL).Msg("serving SkillHub data from server cache")
		web.OK(w, r, json.RawMessage(cached))
		return
	}
	h.cacheMu.Unlock()

	logger.Log.Info().Str("url", dataURL).Msg("fetching SkillHub data from upstream")

	// Create HTTP client with timeout (large file ~3-5MB, needs more time)
	client := &http.Client{
		Timeout: 2 * time.Minute,
	}

	resp, err := client.Get(dataURL)
	if err != nil {
		logger.Log.Error().Err(err).Str("url", dataURL).Msg("failed to fetch SkillHub data")
		web.Fail(w, r, "FETCH_FAILED", err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Log.Error().Int("status", resp.StatusCode).Str("url", dataURL).Msg("SkillHub data fetch returned non-200")
		web.Fail(w, r, "FETCH_FAILED", "upstream returned "+resp.Status, http.StatusBadGateway)
		return
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to read SkillHub data response")
		web.Fail(w, r, "READ_FAILED", err.Error(), http.StatusInternalServerError)
		return
	}

	// Validate JSON
	if !json.Valid(body) {
		logger.Log.Error().Msg("invalid JSON from SkillHub data source")
		web.Fail(w, r, "INVALID_JSON", "upstream returned invalid JSON", http.StatusBadGateway)
		return
	}

	// Update server-side cache
	h.cacheMu.Lock()
	h.cacheData = json.RawMessage(body)
	h.cacheURL = dataURL
	h.cacheTime = time.Now()
	h.cacheMu.Unlock()

	// Return standard API response format
	web.OK(w, r, json.RawMessage(body))
}

// GetInstalledSkills fetches installed skills from OpenClaw Gateway
// GET /api/v1/skillhub/installed
func (h *SkillHubHandler) GetInstalledSkills(w http.ResponseWriter, r *http.Request) {
	// Check if Gateway client is available
	if h.gwClient == nil {
		logger.Log.Debug().Msg("Gateway client not available, returning empty installed skills list")
		web.OK(w, r, map[string]interface{}{
			"skills": []string{},
		})
		return
	}

	// Call OpenClaw Gateway skills.status RPC
	raw, err := h.gwClient.Request("skills.status", map[string]interface{}{})
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to fetch skills status from Gateway")
		web.Fail(w, r, "GATEWAY_ERROR", err.Error(), http.StatusBadGateway)
		return
	}

	// Parse the response
	var response struct {
		Skills []struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		} `json:"skills"`
	}

	if err := json.Unmarshal(raw, &response); err != nil {
		logger.Log.Error().Err(err).Msg("failed to parse skills status response")
		web.Fail(w, r, "PARSE_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	// Log all skills for debugging
	logger.Log.Debug().Int("total", len(response.Skills)).Msg("received skills from Gateway")

	// Filter for managed skills (installed via SkillHub/ClawHub)
	installedSkills := []string{}
	allSources := make(map[string]int)
	for _, skill := range response.Skills {
		allSources[skill.Source]++
		logger.Log.Debug().Str("name", skill.Name).Str("source", skill.Source).Msg("skill info")
		if skill.Source == "openclaw-managed" {
			installedSkills = append(installedSkills, skill.Name)
		}
	}

	logger.Log.Debug().
		Int("count", len(installedSkills)).
		Strs("skills", installedSkills).
		Interface("sources", allSources).
		Msg("fetched installed skills")

	web.OK(w, r, map[string]interface{}{
		"skills": installedSkills,
	})
}
