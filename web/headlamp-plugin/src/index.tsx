/*
 * Copyright 2025 The Kubernetes Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Scaffolding only - the real pages (Dashboard, Compare, TestCase/
// AuthentikTopology detail metrics sections, "Run now") are follow-up work.
// Verified live before writing any of that: this plugin loads and hot-reloads
// via Headlamp's pluginsDir (make headlamp-plugin), and reaches cmd/apiserver
// (deployed in-cluster - make deploy-apiserver) through Headlamp's own
// ApiProxy.request() against the standard Kubernetes Service-proxy
// subresource, e.g.:
//
//   ApiProxy.request(
//     '/api/v1/namespaces/authentik-bench/services/apiserver:8080/proxy/api/v1/topologies',
//     {}, false, true
//   )
//
// cmd/apiserver's BENCH_API_TOKEN must be sent as X-Bench-Api-Token, not
// Authorization - that header is overwritten by Headlamp's own proxy layer
// with its own Kubernetes bearer token before the request reaches the
// proxied service (see internal/api/auth.go's tokenHeader).
