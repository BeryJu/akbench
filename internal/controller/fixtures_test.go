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

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEnsureFixturesSecretCreatesOnRemote(t *testing.T) {
	source := fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).WithObjects(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: fixturesSecretName, Namespace: "default"},
		Data:       map[string][]byte{"users.json": []byte(`[{"username":"a"}]`)},
	}).Build()
	remote := fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).Build()

	if err := ensureFixturesSecret(context.Background(), source, remote, "default"); err != nil {
		t.Fatalf("ensureFixturesSecret: %v", err)
	}

	var mirrored corev1.Secret
	if err := remote.Get(context.Background(), types.NamespacedName{Name: fixturesSecretName, Namespace: "default"}, &mirrored); err != nil {
		t.Fatalf("get mirrored secret: %v", err)
	}
	if string(mirrored.Data["users.json"]) != `[{"username":"a"}]` {
		t.Errorf("mirrored secret data = %q, want the source's users.json", mirrored.Data["users.json"])
	}
}

func TestEnsureFixturesSecretUpdatesExistingOnRemote(t *testing.T) {
	source := fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).WithObjects(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: fixturesSecretName, Namespace: "default"},
		Data:       map[string][]byte{"users.json": []byte("new")},
	}).Build()
	remote := fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).WithObjects(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: fixturesSecretName, Namespace: "default"},
		Data:       map[string][]byte{"users.json": []byte("stale")},
	}).Build()

	if err := ensureFixturesSecret(context.Background(), source, remote, "default"); err != nil {
		t.Fatalf("ensureFixturesSecret: %v", err)
	}

	var mirrored corev1.Secret
	if err := remote.Get(context.Background(), types.NamespacedName{Name: fixturesSecretName, Namespace: "default"}, &mirrored); err != nil {
		t.Fatalf("get mirrored secret: %v", err)
	}
	if string(mirrored.Data["users.json"]) != "new" {
		t.Errorf("mirrored secret data = %q, want refreshed to \"new\"", mirrored.Data["users.json"])
	}
}

func TestEnsureFixturesSecretMissingOnSource(t *testing.T) {
	source := fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).Build()
	remote := fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).Build()

	if err := ensureFixturesSecret(context.Background(), source, remote, "default"); err == nil {
		t.Fatal("expected an error when the source secret doesn't exist, got nil")
	}
}
