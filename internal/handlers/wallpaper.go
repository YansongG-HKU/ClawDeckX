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

type bingImageArchiveResponse struct {
	Images []struct {
		URLBase       string `json:"urlbase"`
		URL           string `json:"url"`
		Title         string `json:"title"`
		Copyright     string `json:"copyright"`
		StartDate     string `json:"startdate"`
		FullStartDate string `json:"fullstartdate"`
	} `json:"images"`
}

type unsplashRandomResponse struct {
	URLs struct {
		Regular string `json:"regular"`
		Full    string `json:"full"`
		Raw     string `json:"raw"`
	} `json:"urls"`
	Description    string `json:"description"`
	AltDescription string `json:"alt_description"`
	User           struct {
		Name string `json:"name"`
	} `json:"user"`
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

func (h *WallpaperHandler) BingDaily(w http.ResponseWriter, r *http.Request) {
	apiURL := "https://cn.bing.com/HPImageArchive.aspx?format=js&idx=0&n=8&mkt=zh-CN"
	resp, err := h.client.Get(apiURL)
	if err != nil {
		web.Fail(w, r, "WALLPAPER_UPSTREAM_FAILED", err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		web.Fail(w, r, "WALLPAPER_UPSTREAM_FAILED", fmt.Sprintf("bing returned status %d", resp.StatusCode), http.StatusBadGateway)
		return
	}

	var payload bingImageArchiveResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		web.Fail(w, r, "WALLPAPER_INVALID_RESPONSE", err.Error(), http.StatusBadGateway)
		return
	}
	if len(payload.Images) == 0 {
		web.Fail(w, r, "WALLPAPER_NOT_FOUND", "no bing wallpaper found", http.StatusNotFound)
		return
	}

	excluded := map[string]bool{}
	for _, raw := range r.URL.Query()["exclude"] {
		value := strings.TrimSpace(raw)
		if value != "" {
			excluded[value] = true
		}
	}

	candidates := make([]struct {
		URLBase       string `json:"urlbase"`
		URL           string `json:"url"`
		Title         string `json:"title"`
		Copyright     string `json:"copyright"`
		StartDate     string `json:"startdate"`
		FullStartDate string `json:"fullstartdate"`
	}, 0, len(payload.Images))
	for _, item := range payload.Images {
		imageURL := strings.TrimSpace(item.URL)
		if imageURL == "" {
			imageURL = strings.TrimSpace(item.URLBase)
			if imageURL != "" {
				imageURL += "_UHD.jpg"
			}
		}
		if imageURL == "" {
			continue
		}
		if strings.HasPrefix(imageURL, "/") {
			imageURL = "https://cn.bing.com" + imageURL
		}
		if excluded[imageURL] {
			continue
		}
		candidates = append(candidates, item)
	}
	if len(candidates) == 0 {
		candidates = payload.Images
	}

	image := candidates[h.rng.Intn(len(candidates))]
	imageURL := strings.TrimSpace(image.URL)
	if imageURL == "" {
		imageURL = strings.TrimSpace(image.URLBase)
		if imageURL != "" {
			imageURL += "_UHD.jpg"
		}
	}
	if imageURL == "" {
		web.Fail(w, r, "WALLPAPER_NOT_FOUND", "bing wallpaper url is empty", http.StatusNotFound)
		return
	}
	if strings.HasPrefix(imageURL, "/") {
		imageURL = "https://cn.bing.com" + imageURL
	}

	web.OK(w, r, map[string]any{
		"provider":        "bing",
		"image_url":       imageURL,
		"title":           image.Title,
		"copyright":       image.Copyright,
		"start_date":      image.StartDate,
		"full_start_date": image.FullStartDate,
	})
}

func (h *WallpaperHandler) UnsplashRandom(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		query = "wallpaper landscape"
	}

	apiURL := fmt.Sprintf("https://unsplash.com/napi/photos/random?orientation=landscape&query=%s", url.QueryEscape(query))
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, apiURL, nil)
	if err != nil {
		web.Fail(w, r, "WALLPAPER_UPSTREAM_FAILED", err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", "https://unsplash.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := h.client.Do(req)
	if err != nil {
		web.Fail(w, r, "WALLPAPER_UPSTREAM_FAILED", err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		web.Fail(w, r, "WALLPAPER_UPSTREAM_FAILED", fmt.Sprintf("unsplash returned status %d", resp.StatusCode), http.StatusBadGateway)
		return
	}

	var payload unsplashRandomResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		web.Fail(w, r, "WALLPAPER_INVALID_RESPONSE", err.Error(), http.StatusBadGateway)
		return
	}

	imageURL := strings.TrimSpace(payload.URLs.Regular)
	if imageURL == "" {
		imageURL = strings.TrimSpace(payload.URLs.Full)
	}
	if imageURL == "" {
		imageURL = strings.TrimSpace(payload.URLs.Raw)
	}
	if imageURL == "" {
		web.Fail(w, r, "WALLPAPER_NOT_FOUND", "no unsplash wallpaper found", http.StatusNotFound)
		return
	}

	title := strings.TrimSpace(payload.Description)
	if title == "" {
		title = strings.TrimSpace(payload.AltDescription)
	}

	web.OK(w, r, map[string]any{
		"provider":     "unsplash",
		"image_url":    imageURL,
		"title":        title,
		"photographer": payload.User.Name,
	})
}

// allowedProxyHosts is the set of upstream hosts we allow proxying images from.
var allowedProxyHosts = map[string]bool{
	"w.wallhaven.cc":      true,
	"th.wallhaven.cc":     true,
	"cn.bing.com":         true,
	"bing.com":            true,
	"images.unsplash.com": true,
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
