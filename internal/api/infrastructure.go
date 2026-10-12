package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/appliance"
)

// The provider-agnostic half of the Infrastructure page: which Cluster API
// infrastructure providers are installed (clusterctl's own inventory) and,
// inside the appliance, enabling more of them. Each provider's connection
// settings get their own endpoints next to this file (incus.go for Incus).

// infraProviderInfo is what the UI knows about an infrastructure provider
// beyond clusterctl's inventory.
type infraProviderInfo struct {
	// ID is the provider's clusterctl name, e.g. "incus".
	ID          string
	DisplayName string
	// ClusterProvider is the WorkloadCluster spec.provider value that
	// provisions through it; empty for a provider akbench can install but
	// not provision with yet.
	ClusterProvider benchv1alpha1.InfrastructureProvider
}

// knownInfraProviders is every infrastructure provider WorkloadCluster can
// provision with. Adding e.g. AWS means a new entry here, a new
// spec.provider case in the operator, and its own credentials endpoints.
var knownInfraProviders = []infraProviderInfo{
	{ID: "incus", DisplayName: "Incus / LXC", ClusterProvider: benchv1alpha1.InfrastructureIncus},
}

var clusterctlProviderListGVK = schema.GroupVersionKind{
	Group: "clusterctl.cluster.x-k8s.io", Version: "v1alpha3", Kind: "ProviderList",
}

type infrastructureProvider struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	// ClusterProvider is the WorkloadCluster spec.provider value this
	// provider backs; empty when akbench can't provision with it yet.
	ClusterProvider string `json:"clusterProvider,omitempty"`
	// InstalledVersion is empty when the provider isn't installed.
	InstalledVersion string `json:"installedVersion,omitempty"`
	// AvailableVersion is the version the appliance would install; empty
	// outside the appliance.
	AvailableVersion string `json:"availableVersion,omitempty"`
	// Enabled is true once the provider was requested in the appliance.
	Enabled bool `json:"enabled"`
}

type infrastructureView struct {
	// Managed is true inside the appliance, where providers are enabled from
	// the UI. Elsewhere they're installed with clusterctl by hand and this
	// page only reports them.
	Managed   bool                     `json:"managed"`
	Providers []infrastructureProvider `json:"providers"`
	// SyncError/SyncedAt are the appliance's last provider sync.
	SyncError string `json:"syncError,omitempty"`
	SyncedAt  string `json:"syncedAt,omitempty"`
}

func (s *Server) getInfrastructure(w http.ResponseWriter, r *http.Request) {
	view, err := s.infrastructure(r)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) infrastructure(r *http.Request) (*infrastructureView, error) {
	view := &infrastructureView{Managed: s.cfg.Appliance}
	byID := map[string]*infrastructureProvider{}
	get := func(id string) *infrastructureProvider {
		if p, ok := byID[id]; ok {
			return p
		}
		p := &infrastructureProvider{ID: id, DisplayName: id}
		byID[id] = p
		return p
	}
	for _, known := range knownInfraProviders {
		p := get(known.ID)
		p.DisplayName = known.DisplayName
		p.ClusterProvider = string(known.ClusterProvider)
	}

	// clusterctl's inventory - absent entirely until Cluster API itself is
	// installed, which just means nothing is.
	inventory := &unstructured.UnstructuredList{}
	inventory.SetGroupVersionKind(clusterctlProviderListGVK)
	if err := s.client.List(r.Context(), inventory); err == nil {
		for _, item := range inventory.Items {
			if t, _, _ := unstructured.NestedString(item.Object, "type"); t != "InfrastructureProvider" {
				continue
			}
			name, _, _ := unstructured.NestedString(item.Object, "providerName")
			version, _, _ := unstructured.NestedString(item.Object, "version")
			get(name).InstalledVersion = version
		}
	} else if !isNoMatch(err) {
		return nil, err
	}

	if s.cfg.Appliance {
		enabled, err := s.enabledProviders(r)
		if err != nil {
			return nil, err
		}
		for _, id := range enabled {
			get(id).Enabled = true
		}
		var status corev1.ConfigMap
		err = s.client.Get(r.Context(), types.NamespacedName{Namespace: appliance.SystemNamespace, Name: appliance.StatusConfigMap}, &status)
		if err != nil && !apierrors.IsNotFound(err) {
			return nil, err
		}
		shipped := map[string]string{}
		_ = json.Unmarshal([]byte(status.Data[appliance.StatusKeyShipped]), &shipped)
		for id, version := range shipped {
			get(id).AvailableVersion = version
		}
		view.SyncError = status.Data[appliance.StatusKeyError]
		view.SyncedAt = status.Data[appliance.StatusKeySyncedAt]
	}

	for _, p := range byID {
		view.Providers = append(view.Providers, *p)
	}
	slices.SortFunc(view.Providers, func(a, b infrastructureProvider) int { return strings.Compare(a.DisplayName, b.DisplayName) })
	return view, nil
}

func (s *Server) enabledProviders(r *http.Request) ([]string, error) {
	var cm corev1.ConfigMap
	err := s.client.Get(r.Context(), types.NamespacedName{Namespace: appliance.SystemNamespace, Name: appliance.ProvidersConfigMap}, &cm)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return appliance.ParseProviderList(cm.Data[appliance.ProvidersKeyInfrastructure]), nil
}

// enableInfrastructureProvider asks the appliance to install a provider.
// There is deliberately no "disable": uninstalling a provider would orphan
// every cluster it provisioned.
func (s *Server) enableInfrastructureProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.cfg.Appliance {
		writeError(w, r, http.StatusConflict,
			"infrastructure providers are installed with clusterctl outside the appliance, e.g. `clusterctl init --infrastructure "+id+"`")
		return
	}
	view, err := s.infrastructure(r)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	idx := slices.IndexFunc(view.Providers, func(p infrastructureProvider) bool { return p.ID == id })
	if idx < 0 || (view.Providers[idx].AvailableVersion == "" && view.Providers[idx].InstalledVersion == "") {
		writeError(w, r, http.StatusNotFound, "this appliance image doesn't ship the infrastructure provider "+id)
		return
	}

	enabled, err := s.enabledProviders(r)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	if !slices.Contains(enabled, id) {
		enabled = append(enabled, id)
		value := strings.Join(enabled, ",")
		var cm corev1.ConfigMap
		key := types.NamespacedName{Namespace: appliance.SystemNamespace, Name: appliance.ProvidersConfigMap}
		switch err := s.client.Get(r.Context(), key, &cm); {
		case apierrors.IsNotFound(err):
			cm = corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
				Data:       map[string]string{appliance.ProvidersKeyInfrastructure: value},
			}
			err = s.client.Create(r.Context(), &cm)
			if err != nil {
				writeAPIError(w, r, err)
				return
			}
		case err != nil:
			writeAPIError(w, r, err)
			return
		default:
			if cm.Data == nil {
				cm.Data = map[string]string{}
			}
			cm.Data[appliance.ProvidersKeyInfrastructure] = value
			if err := s.client.Update(r.Context(), &cm); err != nil {
				writeAPIError(w, r, err)
				return
			}
		}
		view.Providers[idx].Enabled = true
	}
	writeJSON(w, r, http.StatusOK, view)
}

// isNoMatch reports a list against a kind whose CRD isn't installed.
func isNoMatch(err error) bool {
	return meta.IsNoMatchError(err) || runtime.IsNotRegisteredError(err)
}
