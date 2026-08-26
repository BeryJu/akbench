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

package render

import (
	"encoding/json"
	"fmt"
	"maps"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

// MergeExtraValues overlays extraValuesYAML - a "values.yaml"-shaped
// document, from AuthentikTopologySpec.Authentik.ExtraValuesRef - on top of
// base, deep-merging nested maps and letting the overlay win on conflicts.
// This is the escape hatch for chart values the CRD doesn't model directly,
// so the web UI's YAML editor doesn't need a dedicated field for every
// authentik setting.
func MergeExtraValues(base *apiextensionsv1.JSON, extraValuesYAML string) (*apiextensionsv1.JSON, error) {
	var baseMap map[string]any
	if err := json.Unmarshal(base.Raw, &baseMap); err != nil {
		return nil, fmt.Errorf("unmarshal base values: %w", err)
	}

	var extra map[string]any
	if err := yaml.Unmarshal([]byte(extraValuesYAML), &extra); err != nil {
		return nil, fmt.Errorf("parse extra values YAML: %w", err)
	}
	if len(extra) == 0 {
		return base, nil
	}

	raw, err := json.Marshal(deepMergeMaps(baseMap, extra))
	if err != nil {
		return nil, fmt.Errorf("marshal merged values: %w", err)
	}
	return &apiextensionsv1.JSON{Raw: raw}, nil
}

// deepMergeMaps merges overlay onto base: nested maps merge key-by-key
// recursively, anything else (scalars, slices, a map colliding with a
// non-map) is replaced outright by the overlay's value.
func deepMergeMaps(base, overlay map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(overlay))
	maps.Copy(out, base)
	for k, overlayValue := range overlay {
		if baseValue, ok := out[k]; ok {
			baseMap, baseIsMap := baseValue.(map[string]any)
			overlayMap, overlayIsMap := overlayValue.(map[string]any)
			if baseIsMap && overlayIsMap {
				out[k] = deepMergeMaps(baseMap, overlayMap)
				continue
			}
		}
		out[k] = overlayValue
	}
	return out
}
