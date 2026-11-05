package app

import (
	"aether/docs"
	"net/http"
)

func (a *App) releaseNotes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, map[string]string{"version": Version, "body": docs.ReleaseNotes(Version)})
}
