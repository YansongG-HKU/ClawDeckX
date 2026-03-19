package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ClawDeckX/internal/openclaw"
	"ClawDeckX/internal/web"
)

type ConfigBackupHandler struct{}

func NewConfigBackupHandler() *ConfigBackupHandler {
	return &ConfigBackupHandler{}
}

type ConfigBackupFile struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	ModTime  string `json:"modTime"`
	Index    int    `json:"index"` // 0 = .bak, 1 = .bak.1, etc.
}

// List returns all .bak files for the openclaw config.
func (h *ConfigBackupHandler) List(w http.ResponseWriter, r *http.Request) {
	configPath := openclaw.ResolveConfigPath()
	if configPath == "" {
		web.FailErr(w, r, web.ErrInvalidParam, "OpenClaw config path not found")
		return
	}

	backups := h.findBackupFiles(configPath)
	web.OK(w, r, map[string]any{
		"configPath": configPath,
		"backups":    backups,
	})
}

// Preview returns the content of a specific .bak file.
func (h *ConfigBackupHandler) Preview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.FailErr(w, r, web.ErrInvalidBody)
		return
	}

	configPath := openclaw.ResolveConfigPath()
	if configPath == "" {
		web.FailErr(w, r, web.ErrInvalidParam, "OpenClaw config path not found")
		return
	}

	// Security: only allow reading .bak files in the same directory as the config
	if !h.isValidBackupPath(configPath, req.Path) {
		web.FailErr(w, r, web.ErrInvalidParam, "invalid backup path")
		return
	}

	data, err := os.ReadFile(req.Path)
	if err != nil {
		web.FailErr(w, r, web.ErrInvalidParam, fmt.Sprintf("cannot read file: %v", err))
		return
	}

	// Validate it's valid JSON
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		web.OK(w, r, map[string]any{"content": string(data), "valid": false})
		return
	}

	// Re-indent for readability
	pretty, _ := json.MarshalIndent(parsed, "", "  ")
	web.OK(w, r, map[string]any{"content": string(pretty), "valid": true})
}

// Restore replaces the current openclaw.json with a .bak file's content.
func (h *ConfigBackupHandler) Restore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.FailErr(w, r, web.ErrInvalidBody)
		return
	}

	configPath := openclaw.ResolveConfigPath()
	if configPath == "" {
		web.FailErr(w, r, web.ErrInvalidParam, "OpenClaw config path not found")
		return
	}

	if !h.isValidBackupPath(configPath, req.Path) {
		web.FailErr(w, r, web.ErrInvalidParam, "invalid backup path")
		return
	}

	// Read backup file
	bakData, err := os.ReadFile(req.Path)
	if err != nil {
		web.FailErr(w, r, web.ErrInvalidParam, fmt.Sprintf("cannot read backup: %v", err))
		return
	}

	// Validate JSON
	var parsed any
	if err := json.Unmarshal(bakData, &parsed); err != nil {
		web.FailErr(w, r, web.ErrInvalidParam, "backup file contains invalid JSON")
		return
	}

	// Write to config path (OpenClaw will auto-create .bak on next write)
	if err := os.WriteFile(configPath, bakData, 0o600); err != nil {
		web.FailErr(w, r, web.ErrInvalidParam, fmt.Sprintf("cannot write config: %v", err))
		return
	}

	web.OK(w, r, map[string]any{"restored": true})
}

// Diff returns the current config and the backup content for comparison.
func (h *ConfigBackupHandler) Diff(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.FailErr(w, r, web.ErrInvalidBody)
		return
	}

	configPath := openclaw.ResolveConfigPath()
	if configPath == "" {
		web.FailErr(w, r, web.ErrInvalidParam, "OpenClaw config path not found")
		return
	}

	if !h.isValidBackupPath(configPath, req.Path) {
		web.FailErr(w, r, web.ErrInvalidParam, "invalid backup path")
		return
	}

	currentData, err := os.ReadFile(configPath)
	if err != nil {
		web.FailErr(w, r, web.ErrInvalidParam, fmt.Sprintf("cannot read current config: %v", err))
		return
	}

	bakData, err := os.ReadFile(req.Path)
	if err != nil {
		web.FailErr(w, r, web.ErrInvalidParam, fmt.Sprintf("cannot read backup: %v", err))
		return
	}

	// Pretty-print both for diff
	var currentParsed, bakParsed any
	currentPretty := string(currentData)
	bakPretty := string(bakData)
	if json.Unmarshal(currentData, &currentParsed) == nil {
		if b, err := json.MarshalIndent(currentParsed, "", "  "); err == nil {
			currentPretty = string(b)
		}
	}
	if json.Unmarshal(bakData, &bakParsed) == nil {
		if b, err := json.MarshalIndent(bakParsed, "", "  "); err == nil {
			bakPretty = string(b)
		}
	}

	web.OK(w, r, map[string]any{
		"current": currentPretty,
		"backup":  bakPretty,
	})
}

func (h *ConfigBackupHandler) findBackupFiles(configPath string) []ConfigBackupFile {
	dir := filepath.Dir(configPath)
	base := filepath.Base(configPath)
	bakBase := base + ".bak"

	var backups []ConfigBackupFile

	// Check primary .bak
	bakPath := filepath.Join(dir, bakBase)
	if info, err := os.Stat(bakPath); err == nil {
		backups = append(backups, ConfigBackupFile{
			Name:    bakBase,
			Path:    bakPath,
			Size:    info.Size(),
			ModTime: info.ModTime().Format(time.RFC3339),
			Index:   0,
		})
	}

	// Check numbered .bak.1 through .bak.9
	for i := 1; i <= 9; i++ {
		name := fmt.Sprintf("%s.%d", bakBase, i)
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil {
			backups = append(backups, ConfigBackupFile{
				Name:    name,
				Path:    p,
				Size:    info.Size(),
				ModTime: info.ModTime().Format(time.RFC3339),
				Index:   i,
			})
		}
	}

	// Sort by index
	sort.Slice(backups, func(i, j int) bool { return backups[i].Index < backups[j].Index })
	return backups
}

func (h *ConfigBackupHandler) isValidBackupPath(configPath, bakPath string) bool {
	configDir := filepath.Dir(configPath)
	configBase := filepath.Base(configPath)

	// Must be in the same directory
	bakDir := filepath.Dir(bakPath)
	if bakDir != configDir {
		return false
	}

	// Must start with config basename + ".bak"
	bakName := filepath.Base(bakPath)
	if !strings.HasPrefix(bakName, configBase+".bak") {
		return false
	}

	// Must actually exist
	if _, err := os.Stat(bakPath); err != nil {
		return false
	}

	return true
}
