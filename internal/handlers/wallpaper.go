package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ClawDeckX/internal/web"
)

type WallpaperHandler struct {
	client *http.Client
	rng    *rand.Rand
}

type wallhavenSearchResponse struct {
	Data []struct {
		ID         string `json:"id"`
		URL        string `json:"url"`
		Path       string `json:"path"`
		Resolution string `json:"resolution"`
		Ratio      string `json:"ratio"`
		Category   string `json:"category"`
		Purity     string `json:"purity"`
		Thumbs     struct {
			Large string `json:"large"`
			Small string `json:"small"`
		} `json:"thumbs"`
		Colors []string `json:"colors"`
	} `json:"data"`
	Meta struct {
		CurrentPage int    `json:"current_page"`
		LastPage    int    `json:"last_page"`
		PerPage     int    `json:"per_page"`
		Total       int    `json:"total"`
		Seed        string `json:"seed"`
	} `json:"meta"`
}

func NewWallpaperHandler() *WallpaperHandler {
	return &WallpaperHandler{
		client: &http.Client{Timeout: 20 * time.Second},
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (h *WallpaperHandler) WallhavenRandom(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	atLeast := strings.TrimSpace(r.URL.Query().Get("atleast"))
	if atLeast == "" {
		atLeast = "1920x1080"
	}
	ratio := strings.TrimSpace(r.URL.Query().Get("ratios"))
	if ratio == "" {
		ratio = "16x9,16x10,21x9"
	}
	categories := strings.TrimSpace(r.URL.Query().Get("categories"))
	if categories == "" {
		categories = "110"
	}
	purity := strings.TrimSpace(r.URL.Query().Get("purity"))
	if purity == "" {
		purity = "100"
	}
	page := 1
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			page = parsed
		}
	}

	params := url.Values{}
	params.Set("sorting", "random")
	params.Set("categories", categories)
	params.Set("purity", purity)
	params.Set("atleast", atLeast)
	params.Set("ratios", ratio)
	params.Set("page", strconv.Itoa(page))
	if query != "" {
		params.Set("q", query)
	}
	if seed := strings.TrimSpace(r.URL.Query().Get("seed")); seed != "" {
		params.Set("seed", seed)
	}
	if apiKey := strings.TrimSpace(r.URL.Query().Get("apikey")); apiKey != "" {
		params.Set("apikey", apiKey)
	}

	apiURL := fmt.Sprintf("https://wallhaven.cc/api/v1/search?%s", params.Encode())
	resp, err := h.client.Get(apiURL)
	if err != nil {
		web.Fail(w, r, "WALLPAPER_UPSTREAM_FAILED", err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		web.Fail(w, r, "WALLPAPER_UPSTREAM_FAILED", fmt.Sprintf("wallhaven returned status %d", resp.StatusCode), http.StatusBadGateway)
		return
	}

	var payload wallhavenSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		web.Fail(w, r, "WALLPAPER_INVALID_RESPONSE", err.Error(), http.StatusBadGateway)
		return
	}
	if len(payload.Data) == 0 {
		web.Fail(w, r, "WALLPAPER_NOT_FOUND", "no wallpaper matched the current filters", http.StatusNotFound)
		return
	}

	picked := payload.Data[h.rng.Intn(len(payload.Data))]
	web.OK(w, r, map[string]any{
		"provider":   "wallhaven",
		"id":         picked.ID,
		"url":        picked.URL,
		"image_url":  picked.Path,
		"thumb_url":  picked.Thumbs.Large,
		"resolution": picked.Resolution,
		"ratio":      picked.Ratio,
		"category":   picked.Category,
		"purity":     picked.Purity,
		"colors":     picked.Colors,
		"seed":       payload.Meta.Seed,
		"page":       payload.Meta.CurrentPage,
		"total":      payload.Meta.Total,
	})
}

// allowedProxyHosts is the set of upstream hosts we allow proxying images from.
var allowedProxyHosts = map[string]bool{
	"w.wallhaven.cc":  true,
	"th.wallhaven.cc": true,
}

// ImageProxy fetches a remote wallpaper image server-side, adding the correct
// Referer and User-Agent headers that Wallhaven's CDN requires. Only whitelisted
// hosts are proxied to prevent open-redirect / SSRF issues.
func (h *WallpaperHandler) ImageProxy(w http.ResponseWriter, r *http.Request) {
	rawURL := strings.TrimSpace(r.URL.Query().Get("url"))
	if rawURL == "" {
		web.Fail(w, r, "WALLPAPER_PROXY_MISSING_URL", "url query parameter is required", http.StatusBadRequest)
		return
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		web.Fail(w, r, "WALLPAPER_PROXY_INVALID_URL", "invalid image URL", http.StatusBadRequest)
		return
	}

	if !allowedProxyHosts[parsed.Host] {
		web.Fail(w, r, "WALLPAPER_PROXY_HOST_DENIED", fmt.Sprintf("host %q is not allowed", parsed.Host), http.StatusForbidden)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, rawURL, nil)
	if err != nil {
		web.Fail(w, r, "WALLPAPER_PROXY_FAILED", err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Referer", "https://wallhaven.cc/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := h.client.Do(req)
	if err != nil {
		web.Fail(w, r, "WALLPAPER_PROXY_FAILED", err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		web.Fail(w, r, "WALLPAPER_PROXY_UPSTREAM", fmt.Sprintf("upstream returned %d", resp.StatusCode), http.StatusBadGateway)
		return
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/jpeg"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}

	io.Copy(w, resp.Body)
}
