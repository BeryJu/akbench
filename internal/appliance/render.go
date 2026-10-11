/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
