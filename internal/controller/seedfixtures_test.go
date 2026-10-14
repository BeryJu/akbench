package controller

import (
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/authentik-labs/akbench/internal/fixtures"
)

func seedClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func TestSeedFixturesCreatesMatchingPair(t *testing.T) {
	c := seedClient(t)
	if err := SeedFixtures(t.Context(), c, "default", 12, 3); err != nil {
		t.Fatal(err)
	}
	var secret corev1.Secret
	if err := c.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: fixturesSecretName}, &secret); err != nil {
		t.Fatal(err)
	}
	var cm corev1.ConfigMap
	if err := c.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: fixturesBlueprintConfigMapName}, &cm); err != nil {
		t.Fatal(err)
	}
	var creds []fixtures.Credential
	if err := json.Unmarshal(secret.Data[fixturesUsersKey], &creds); err != nil || len(creds) != 12 {
		t.Fatalf("users.json: %d credentials, err %v", len(creds), err)
	}
	// Every credential's user is in the blueprint the deployments import.
	for _, cred := range creds {
		if !strings.Contains(cm.Data[fixturesBlueprintKey], "username: "+cred.Username+"\n") {
			t.Errorf("blueprint has no user %q", cred.Username)
		}
	}

	// Seeding again (operator restart) changes nothing.
	before := string(secret.Data[fixturesUsersKey])
	if err := SeedFixtures(t.Context(), c, "default", 12, 99); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: fixturesSecretName}, &secret); err != nil {
		t.Fatal(err)
	}
	if string(secret.Data[fixturesUsersKey]) != before {
		t.Error("existing test users replaced")
	}
}

func TestSeedFixturesLeavesHalfLoadedFixturesAlone(t *testing.T) {
	// Users loaded by hand without a blueprint: generating a blueprint now
	// would describe different users, so nothing is created.
	c := seedClient(t, &corev1.Secret{Name: fixturesSecretName, Namespace: "default", Data: map[string][]byte{fixturesUsersKey: []byte("[]")}})
	if err := SeedFixtures(t.Context(), c, "default", 12, 3); err != nil {
		t.Fatal(err)
	}
	err := c.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: fixturesBlueprintConfigMapName}, &corev1.ConfigMap{})
	if !apierrors.IsNotFound(err) {
		t.Errorf("blueprint ConfigMap created next to an existing Secret (err %v)", err)
	}
}
