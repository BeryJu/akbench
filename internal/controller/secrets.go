package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ensureRandomSecret gets-or-creates a Secret holding a randomly generated
// value under dataKey, mirroring the Ansible harness's behaviour of
// generating the authentik secret key and database password once on first
// run and caching them so re-runs don't rotate them. Returns the value
// (existing or newly generated) so callers that need it in plaintext (e.g.
// to also set a matching bundled-postgres password) can use it directly.
func ensureRandomSecret(ctx context.Context, c client.Client, key types.NamespacedName, dataKey string, byteLen int) (string, error) {
	var existing corev1.Secret
	err := c.Get(ctx, key, &existing)
	if err == nil {
		if v, ok := existing.Data[dataKey]; ok && len(v) > 0 {
			return string(v), nil
		}
		value, err := randomHex(byteLen)
		if err != nil {
			return "", err
		}
		if existing.StringData == nil {
			existing.StringData = map[string]string{}
		}
		existing.StringData[dataKey] = value
		return value, c.Update(ctx, &existing)
	}
	if !apierrors.IsNotFound(err) {
		return "", err
	}
	value, err := randomHex(byteLen)
	if err != nil {
		return "", err
	}
	sec := &corev1.Secret{
		Name: key.Name, Namespace: key.Namespace,
		StringData: map[string]string{dataKey: value},
	}
	if err := c.Create(ctx, sec); err != nil {
		return "", fmt.Errorf("create secret %s/%s: %w", key.Namespace, key.Name, err)
	}
	return value, nil
}

func randomHex(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random value: %w", err)
	}
	return hex.EncodeToString(b), nil
}
