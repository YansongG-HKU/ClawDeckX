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
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
	Index   int    `json:"index"` // 0 = .bak, 1 = .bak.1, etc.
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

	// Compute line-level diff
	diffLines := computeLineDiff(currentPretty, bakPretty)

	web.OK(w, r, map[string]any{
		"current":   currentPretty,
		"backup":    bakPretty,
		"diffLines": diffLines,
	})
}

// DiffLine represents a single line in the diff output.
type DiffLine struct {
	Type string `json:"type"` // "equal", "add", "remove"
	Text string `json:"text"`
}

// computeLineDiff computes a line-level diff between current and backup text.
func computeLineDiff(current, backup string) []DiffLine {
	currentLines := strings.Split(current, "\n")
	backupLines := strings.Split(backup, "\n")
	m, n := len(currentLines), len(backupLines)

	// Build LCS table
	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if currentLines[i-1] == backupLines[j-1] {
				lcs[i][j] = lcs[i-1][j-1] + 1
			} else if lcs[i-1][j] >= lcs[i][j-1] {
				lcs[i][j] = lcs[i-1][j]
			} else {
				lcs[i][j] = lcs[i][j-1]
			}
		}
	}

	// Backtrack to build diff
	var result []DiffLine
	i, j := m, n
	var stack []DiffLine
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && currentLines[i-1] == backupLines[j-1] {
			stack = append(stack, DiffLine{Type: "equal", Text: currentLines[i-1]})
			i--
			j--
		} else if j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]) {
			stack = append(stack, DiffLine{Type: "add", Text: backupLines[j-1]})
			j--
		} else {
			stack = append(stack, DiffLine{Type: "remove", Text: currentLines[i-1]})
			i--
		}
	}
	// Reverse
	for k := len(stack) - 1; k >= 0; k-- {
		result = append(result, stack[k])
	}

	// Compact: only keep diff hunks with context (3 lines before/after changes)
	const contextLines = 3
	changed := make([]bool, len(result))
	for idx, dl := range result {
		if dl.Type != "equal" {
			changed[idx] = true
		}
	}
	// Mark context lines around changes
	show := make([]bool, len(result))
	for idx := range result {
		if changed[idx] {
			for c := max(0, idx-contextLines); c <= min(len(result)-1, idx+contextLines); c++ {
				show[c] = true
			}
		}
	}

	var compact []DiffLine
	lastShown := -1
	for idx, dl := range result {
		if !show[idx] {
			continue
		}
		if lastShown >= 0 && idx > lastShown+1 {
			compact = append(compact, DiffLine{Type: "separator", Text: "···"})
		}
		compact = append(compact, dl)
		lastShown = idx
	}

	if len(compact) == 0 {
		return []DiffLine{{Type: "equal", Text: "(no differences)"}}
	}
	return compact
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
