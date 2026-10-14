package api

import (
	"net/http"
	"regexp"

	"github.com/goauthentik/authentik-bench/internal/registry"
)

// imageRegistryHost is the only registry checkImage will query: the one
// authentik's CI publishes its release and per-commit dev images to.
const imageRegistryHost = "ghcr.io"

// Only goauthentik's own repositories, so this endpoint can't be used to
// probe arbitrary registries or images.
var imageRepositoryPattern = regexp.MustCompile(`^goauthentik/[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

// checkImage answers whether ghcr.io/<repository>:<tag> exists. authentik's
// CI only publishes a commit's dev image once its tests pass (and never for
// a cancelled run), so the UI checks a PR's image before deploying it. The
// browser can't ask ghcr.io itself - the registry doesn't allow CORS.
func (s *Server) checkImage(w http.ResponseWriter, r *http.Request) {
	repository := r.URL.Query().Get("repository")
	tag := r.URL.Query().Get("tag")
	if !imageRepositoryPattern.MatchString(repository) {
		writeError(w, r, http.StatusBadRequest, "repository must be a goauthentik/... repository on ghcr.io")
		return
	}
	if !registry.TagPattern.MatchString(tag) {
		writeError(w, r, http.StatusBadRequest, "invalid image tag")
		return
	}
	checker := registry.Checker{HTTP: s.http}
	if s.imageRegistry != "" {
		checker.Endpoints = map[string]string{imageRegistryHost: s.imageRegistry}
	}
	exists, err := checker.TagExists(r.Context(), imageRegistryHost+"/"+repository, tag)
	if err != nil {
		writeError(w, r, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]bool{"exists": exists})
}
