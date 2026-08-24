// Package scripts embeds the built-in k6 scripts a TestCase can reference by
// name (e.g. "login.ts"), so the operator binary carries them without
// needing a separate image or volume mount at build time.
package scripts

import "embed"

//go:embed *.ts
var files embed.FS

// Get returns a built-in script's contents by file name (e.g. "login.ts").
func Get(name string) (string, bool) {
	b, err := files.ReadFile(name)
	if err != nil {
		return "", false
	}
	return string(b), true
}
