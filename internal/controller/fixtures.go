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

package controller

// fixturesSecretName is the Secret every k6 workload mounts users.json from.
// M1: created once per namespace by `make fixtures` (see fixtures/gen-blueprint.py)
// and read directly, since everything is on one cluster. From M4 on the same
// name is what RemoteSecretMirrorController mirrors to every runner cluster.
const fixturesSecretName = "bench-fixtures-users"

// fixturesBlueprintConfigMapName is the ConfigMap `make fixtures` also
// writes (same script, same management-cluster namespace as
// fixturesSecretName) holding the authentik blueprint that creates the
// synthetic users users.json has credentials for. AuthentikTopologyReconciler
// reads this once a topology's HelmRelease is Ready, mirrors it onto the
// runner cluster as a ConfigMap the chart mounts into the server pod, and
// execs `ak apply_blueprint` against it before marking the topology Ready -
// optional (a topology with no ConfigMap by this name just skips the step),
// since fixtures are a dev/benchmark convenience, not a hard dependency.
const (
	fixturesBlueprintConfigMapName = "bench-fixtures-blueprint"
	fixturesBlueprintKey           = "blueprint.yaml"
)
