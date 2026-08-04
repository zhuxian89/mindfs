package app

import (
	"net/http"
	"path"
	"strings"

	"mindfs-cloud/internal/config"
)

func (a *App) handleReady(w http.ResponseWriter, r *http.Request) {
	if !a.isReady(r) {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unready"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (a *App) isReady(r *http.Request) bool {
	return a.store.Ping(r.Context()) == nil && config.CheckAssetsDir(a.config.AssetsDir) == nil
}

func (a *App) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := a.metrics.Render(w, a.isReady(r)); err != nil {
		return
	}
}

func (a *App) handleAsset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/mindfs-assets/")
	if name == "" || name == "." || strings.Contains(name, "\\") || path.Clean(name) != name {
		assetNotFound(w, r)
		return
	}
	file, err := a.assets.Open(name)
	if err != nil {
		assetNotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		assetNotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, path.Base(name), info.ModTime(), file)
}

func assetNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.NotFound(w, r)
}
