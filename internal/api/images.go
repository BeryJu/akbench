package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
)

// defaultImageRegistry is the only registry imageExists will query: the one
// authentik's CI publishes its release and per-commit dev images to.
const defaultImageRegistry = "https://ghcr.io"

var (
	// Only goauthentik's own repositories, so this endpoint can't be used to
	// probe arbitrary registries or images.
	imageRepositoryPattern = regexp.MustCompile(`^goauthentik/[a-z0-9]+(?:[._-][a-z0-9]+)*$`)
	// The OCI distribution spec's tag grammar.
	imageTagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
)

// manifestAccept lists every manifest media type a tag may resolve to - a
// multi-arch index for authentik's own images, but any of them counts.
const manifestAccept = "application/vnd.oci.image.index.v1+json, " +
	"application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json, " +
	"application/vnd.docker.distribution.manifest.v2+json"

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
	if !imageTagPattern.MatchString(tag) {
		writeError(w, r, http.StatusBadRequest, "invalid image tag")
		return
	}
	exists, err := s.imageExists(r, repository, tag)
	if err != nil {
		writeError(w, r, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]bool{"exists": exists})
}

func (s *Server) imageExists(r *http.Request, repository, tag string) (bool, error) {
	registry := s.imageRegistry
	if registry == "" {
		registry = defaultImageRegistry
	}
	// Public images still need an (anonymous) bearer token.
	tokenURL := registry + "/token?" + url.Values{"scope": {"repository:" + repository + ":pull"}}.Encode()
	tokenReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, tokenURL, nil)
	if err != nil {
		return false, err
	}
	tokenRes, err := s.http.Do(tokenReq)
	if err != nil {
		return false, fmt.Errorf("registry token request failed: %w", err)
	}
	defer func() { _ = tokenRes.Body.Close() }()
	if tokenRes.StatusCode != http.StatusOK {
		return false, fmt.Errorf("registry token request failed: %s", tokenRes.Status)
	}
	var token struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(tokenRes.Body).Decode(&token); err != nil {
		return false, fmt.Errorf("decode registry token: %w", err)
	}

	manifestURL := fmt.Sprintf("%s/v2/%s/manifests/%s", registry, repository, tag)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodHead, manifestURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token.Token)
	req.Header.Set("Accept", manifestAccept)
	res, err := s.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("registry manifest request failed: %w", err)
	}
	_ = res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("registry manifest request failed: %s", res.Status)
	}
}
