package api

import (
	"net/http"
	"runtime/debug"
)

// Version and Commit are set at build time with -ldflags "-X ..."
// (Dockerfile.apiserver's VERSION/COMMIT build args). The Docker build
// context has no .git, so Go's own VCS stamping can't fill Commit in there.
var (
	Version = "dev"
	Commit  = ""
)

// BuildInfo identifies the running apiserver build - shown in the UI's
// sidebar so it's always clear which build a screenshot or bug report is from.
type BuildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	Dirty     bool   `json:"dirty,omitempty"`
	GoVersion string `json:"goVersion,omitempty"`
}

// currentBuildInfo prefers the linker-set Version/Commit and falls back to
// the VCS info Go embeds itself for a build from a git checkout (`go run`,
// `go build` locally), which also knows whether the tree had local changes.
func currentBuildInfo() BuildInfo {
	info := BuildInfo{Version: Version, Commit: Commit}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	info.GoVersion = bi.GoVersion
	vcsRevision := ""
	modified := false
	for _, setting := range bi.Settings {
		switch setting.Key {
		case "vcs.revision":
			vcsRevision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if info.Commit == "" && vcsRevision != "" {
		info.Commit = vcsRevision
		info.Dirty = modified
	}
	return info
}

func (s *Server) getVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, currentBuildInfo())
}
