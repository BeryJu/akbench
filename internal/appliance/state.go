package appliance

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const tokenFile = "api-token"

// State is what the appliance generates once and then keeps for the
// lifetime of its volume.
type State struct {
	APIToken string
}

// EnsureState loads the persisted state from cfg.StateDir, generating any
// missing piece. Safe to call on every boot.
func EnsureState(cfg *Config) (*State, error) {
	if cfg.APIToken != "" {
		return &State{APIToken: cfg.APIToken}, nil
	}
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(cfg.StateDir, tokenFile)
	token, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		token = []byte(hex.EncodeToString(b))
		err = os.WriteFile(path, token, 0o600)
	}
	if err != nil {
		return nil, fmt.Errorf("api token: %w", err)
	}
	return &State{APIToken: strings.TrimSpace(string(token))}, nil
}
