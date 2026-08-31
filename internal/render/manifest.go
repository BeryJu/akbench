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
	"bytes"
	"errors"
	"fmt"
	"io"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// decodeManifest splits a multi-document vendored YAML manifest (CNI,
// storage, metrics-server - anything embedded verbatim from a third-party
// release rather than rendered from Go structs) into the objects
// WorkloadClusterReconciler's createAll applies one by one.
func decodeManifest(data []byte) ([]client.Object, error) {
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var objs []client.Object
	for {
		u := &unstructured.Unstructured{}
		if err := decoder.Decode(u); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode manifest: %w", err)
		}
		if len(u.Object) == 0 {
			continue
		}
		objs = append(objs, u)
	}
	return objs, nil
}
