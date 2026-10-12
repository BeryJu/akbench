package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/incus"
)

// Incus connections, managed from the Infrastructure page: each is a Secret
// in the bench namespace in exactly the shape a WorkloadCluster's
// spec.incus.lxcSecretRef expects (internal/incus), so the operator and
// cluster-api-provider-incus need no UI-specific handling. The client key
// is write-only - it never leaves the API once stored.

// credentialsLabel marks a Secret as an infrastructure provider's
// credentials; its value is the provider ID ("incus").
const credentialsLabel = "bench.goauthentik.io/infrastructure-credentials"

const incusProviderID = "incus"

type incusCredential struct {
	Name               string `json:"name"`
	Server             string `json:"server"`
	Project            string `json:"project,omitempty"`
	ServerCert         string `json:"serverCert,omitempty"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify"`
	// ClientCert is the certificate the Incus server has to trust; safe to
	// show (and to copy onto the server).
	ClientCert            string `json:"clientCert,omitempty"`
	ClientCertFingerprint string `json:"clientCertFingerprint,omitempty"`
	HasClientKey          bool   `json:"hasClientKey"`
	// UsedBy lists the WorkloadClusters provisioning through this
	// connection.
	UsedBy []string `json:"usedBy"`
	// Labeled is false for a Secret created outside the UI that a
	// WorkloadCluster references (e.g. a migrated one); saving it from the
	// UI labels it.
	Labeled bool `json:"labeled"`
}

type incusCredentialInput struct {
	Name               string `json:"name"`
	Server             string `json:"server"`
	Project            string `json:"project"`
	ServerCert         string `json:"serverCert"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify"`
	// ClientCert/ClientKey bring an existing client certificate; left empty,
	// a new one is generated on create and the stored one kept on update.
	ClientCert string `json:"clientCert"`
	ClientKey  string `json:"clientKey"`
	// RegenerateClientCert replaces the stored client certificate with a
	// freshly generated one (which then needs trusting again).
	RegenerateClientCert bool `json:"regenerateClientCert"`
}

func (s *Server) incusUsers(r *http.Request) (map[string][]string, error) {
	users := map[string][]string{}
	var clusters benchv1alpha1.WorkloadClusterList
	if err := s.client.List(r.Context(), &clusters, client.InNamespace(s.cfg.Namespace)); err != nil {
		// Before the operator's CRDs are installed (e.g. an appliance still
		// bootstrapping), no cluster can use a connection yet - which
		// mustn't stop connections from being set up in the meantime.
		if isNoMatch(err) {
			return users, nil
		}
		return nil, err
	}
	for _, wc := range clusters.Items {
		if wc.Spec.Incus != nil && wc.Spec.Incus.LXCSecretRef.Name != "" {
			users[wc.Spec.Incus.LXCSecretRef.Name] = append(users[wc.Spec.Incus.LXCSecretRef.Name], wc.Name)
		}
	}
	return users, nil
}

func toIncusCredential(secret *corev1.Secret, users map[string][]string) incusCredential {
	c := incus.FromSecret(secret)
	used := users[secret.Name]
	if used == nil {
		used = []string{}
	}
	slices.Sort(used)
	return incusCredential{
		Name:                  secret.Name,
		Server:                c.Server,
		Project:               c.Project,
		ServerCert:            c.ServerCert,
		InsecureSkipVerify:    c.InsecureSkipVerify,
		ClientCert:            c.ClientCert,
		ClientCertFingerprint: incus.Fingerprint(c.ClientCert),
		HasClientKey:          c.ClientKey != "",
		UsedBy:                used,
		Labeled:               secret.Labels[credentialsLabel] == incusProviderID,
	}
}

// listIncusCredentials returns every labeled Incus connection Secret plus
// any other Secret a WorkloadCluster names as its connection.
func (s *Server) listIncusCredentials(w http.ResponseWriter, r *http.Request) {
	users, err := s.incusUsers(r)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	var labeled corev1.SecretList
	if err := s.client.List(r.Context(), &labeled, client.InNamespace(s.cfg.Namespace),
		client.MatchingLabels{credentialsLabel: incusProviderID}); err != nil {
		writeAPIError(w, r, err)
		return
	}
	items := []incusCredential{}
	seen := map[string]bool{}
	for i := range labeled.Items {
		items = append(items, toIncusCredential(&labeled.Items[i], users))
		seen[labeled.Items[i].Name] = true
	}
	for name := range users {
		if seen[name] {
			continue
		}
		var secret corev1.Secret
		err := s.client.Get(r.Context(), types.NamespacedName{Namespace: s.cfg.Namespace, Name: name}, &secret)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			writeAPIError(w, r, err)
			return
		}
		items = append(items, toIncusCredential(&secret, users))
	}
	slices.SortFunc(items, func(a, b incusCredential) int { return strings.Compare(a.Name, b.Name) })
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items})
}

// getIncusSecret fetches a connection Secret the UI may manage: one carrying
// the Incus label, or one a WorkloadCluster references. Any other Secret
// in the namespace is reported as not found, so these endpoints can't be
// used to read or overwrite arbitrary Secrets.
func (s *Server) getIncusSecret(r *http.Request, name string, users map[string][]string) (*corev1.Secret, error) {
	var secret corev1.Secret
	if err := s.client.Get(r.Context(), types.NamespacedName{Namespace: s.cfg.Namespace, Name: name}, &secret); err != nil {
		return nil, err
	}
	if secret.Labels[credentialsLabel] != incusProviderID && len(users[name]) == 0 {
		return nil, apierrors.NewNotFound(corev1.Resource("secrets"), name)
	}
	return &secret, nil
}

func (s *Server) getIncusCredential(w http.ResponseWriter, r *http.Request) {
	users, err := s.incusUsers(r)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	secret, err := s.getIncusSecret(r, chi.URLParam(r, "name"), users)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toIncusCredential(secret, users))
}

func decodeIncusInput(w http.ResponseWriter, r *http.Request) (*incusCredentialInput, bool) {
	var in incusCredentialInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&in); err != nil {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return nil, false
	}
	in.Server = strings.TrimRight(strings.TrimSpace(in.Server), "/")
	in.Project = strings.TrimSpace(in.Project)
	in.ServerCert = strings.TrimSpace(in.ServerCert)
	if (in.ClientCert == "") != (in.ClientKey == "") {
		writeError(w, r, http.StatusBadRequest, "provide both a client certificate and its key, or neither")
		return nil, false
	}
	if in.ClientCert != "" && in.RegenerateClientCert {
		writeError(w, r, http.StatusBadRequest, "either bring a client certificate or regenerate one, not both")
		return nil, false
	}
	return &in, true
}

// apply merges in over existing (nil on create) into a validated connection.
func (in *incusCredentialInput) apply(existing *incus.Connection) (incus.Connection, error) {
	c := incus.Connection{
		Server: in.Server, Project: in.Project, ServerCert: in.ServerCert, InsecureSkipVerify: in.InsecureSkipVerify,
		ClientCert: in.ClientCert, ClientKey: in.ClientKey,
	}
	if c.ClientCert == "" && existing != nil && !in.RegenerateClientCert {
		c.ClientCert, c.ClientKey = existing.ClientCert, existing.ClientKey
	}
	if c.ClientCert == "" {
		var err error
		if c.ClientCert, c.ClientKey, err = incus.GenerateClientCert(); err != nil {
			return c, err
		}
	}
	return c, c.Validate()
}

func (s *Server) createIncusCredential(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeIncusInput(w, r)
	if !ok {
		return
	}
	if errs := validation.IsDNS1123Subdomain(in.Name); len(errs) > 0 {
		writeError(w, r, http.StatusBadRequest, "invalid name: "+strings.Join(errs, "; "))
		return
	}
	conn, err := in.apply(nil)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: in.Name, Namespace: s.cfg.Namespace,
			Labels: map[string]string{credentialsLabel: incusProviderID},
		},
		Type: corev1.SecretTypeOpaque,
		Data: conn.SecretData(),
	}
	if err := s.client.Create(r.Context(), secret); err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, toIncusCredential(secret, nil))
}

func (s *Server) updateIncusCredential(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeIncusInput(w, r)
	if !ok {
		return
	}
	users, err := s.incusUsers(r)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	secret, err := s.getIncusSecret(r, chi.URLParam(r, "name"), users)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	existing := incus.FromSecret(secret)
	conn, err := in.apply(&existing)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if secret.Labels == nil {
		secret.Labels = map[string]string{}
	}
	secret.Labels[credentialsLabel] = incusProviderID
	secret.Data = conn.SecretData()
	if err := s.client.Update(r.Context(), secret); err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, toIncusCredential(secret, users))
}

func (s *Server) deleteIncusCredential(w http.ResponseWriter, r *http.Request) {
	users, err := s.incusUsers(r)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	name := chi.URLParam(r, "name")
	secret, err := s.getIncusSecret(r, name, users)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	if len(users[name]) > 0 {
		writeError(w, r, http.StatusConflict,
			fmt.Sprintf("still used by workload cluster(s) %s - their machines and storage are managed through it", strings.Join(users[name], ", ")))
		return
	}
	if err := s.client.Delete(r.Context(), secret); err != nil {
		writeAPIError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// testIncusCredential reports whether the daemon is reachable and trusts the
// stored client certificate.
func (s *Server) testIncusCredential(w http.ResponseWriter, r *http.Request) {
	users, err := s.incusUsers(r)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	secret, err := s.getIncusSecret(r, chi.URLParam(r, "name"), users)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, incus.Probe(r.Context(), incus.FromSecret(secret)))
}

// trustIncusCredential adds the stored client certificate to the daemon's
// trust store with a token from `incus config trust add <name>`, then
// reports the connection's state like testIncusCredential.
func (s *Server) trustIncusCredential(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&body); err != nil || strings.TrimSpace(body.Token) == "" {
		writeError(w, r, http.StatusBadRequest, "a trust token is required")
		return
	}
	users, err := s.incusUsers(r)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	secret, err := s.getIncusSecret(r, chi.URLParam(r, "name"), users)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	conn := incus.FromSecret(secret)
	if err := incus.TrustWithToken(r.Context(), conn, body.Token); err != nil {
		writeError(w, r, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, incus.Probe(r.Context(), conn))
}
