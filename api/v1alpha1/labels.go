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

package v1alpha1

// Labels shared across bench.goauthentik.io kinds and the workloads they
// render.
const (
	// LabelTestCase names the TestCase a TestRun or a k6 workload belongs to.
	// It is what makes `kubectl get testruns -l
	// bench.goauthentik.io/testcase=<name>` a test case's run history.
	LabelTestCase = "bench.goauthentik.io/testcase"
	// LabelProfile records which profile (quick/burn) a k6 workload runs.
	LabelProfile = "bench.goauthentik.io/profile"
	// LabelTopology names the AuthentikTopology a pod belongs to: set on
	// every one of a topology's own pods (server, worker,
	// embedded/CNPG postgres) and on the k6 runner pod driving it, so
	// cross-topology pod anti-affinity (internal/render/isolation.go) has a
	// single label to match on regardless of which component or postgres
	// mode is involved.
	LabelTopology = "bench.goauthentik.io/topology"
)
