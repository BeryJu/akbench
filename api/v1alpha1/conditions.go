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

// Condition types shared across bench.goauthentik.io kinds.
const (
	// ConditionReady is True once a resource has fully reconciled to its
	// desired state.
	ConditionReady = "Ready"
	// ConditionPostgresReady is True once an AuthentikTopology's postgres
	// backend (embedded/CNPG/external) is reachable.
	ConditionPostgresReady = "PostgresReady"
	// ConditionHelmReleaseReady mirrors the status of the HelmRelease an
	// AuthentikTopology owns.
	ConditionHelmReleaseReady = "HelmReleaseReady"
	// ConditionTopologyReady is True once a TestCase's AuthentikTopology is
	// itself Ready.
	ConditionTopologyReady = "TopologyReady"
	// ConditionScriptReady is True once a TestCase's k6 script has been
	// resolved and mirrored to the runner cluster.
	ConditionScriptReady = "ScriptReady"
)

// Condition reasons shared across bench.goauthentik.io kinds.
const (
	ReasonReconciling        = "Reconciling"
	ReasonReconcileError     = "ReconcileError"
	ReasonReady              = "Ready"
	ReasonNotImplemented     = "NotImplemented"
	ReasonDependencyNotReady = "DependencyNotReady"
)
