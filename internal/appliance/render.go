package appliance

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"text/template"
)

//go:embed manifests/stack.yaml.tmpl
var stackTemplate string

var stackTmpl = template.Must(template.New("stack").Funcs(template.FuncMap{
	"toJSON": func(v any) (string, error) {
		b, err := json.Marshal(v)
		return string(b), err
	},
}).Parse(stackTemplate))

// RenderStack renders the multi-document manifest that installs akbench
// itself (operator HelmRelease, apiserver, Incus/token Secrets) onto the
// appliance's k3s.
func RenderStack(cfg *Config, st *State) ([]byte, error) {
	var buf bytes.Buffer
	if err := stackTmpl.Execute(&buf, struct {
		Config *Config
		State  *State
	}{cfg, st}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
