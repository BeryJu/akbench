package api

import (
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectEvent is one Kubernetes Event about a bench object - the history
// `kubectl describe` shows. Every controller emits one per condition it sets
// (internal/controller/conditions.go's setCondition), so this doubles as a
// log of everything the operator did to the object.
type ObjectEvent struct {
	Type      string `json:"type"`
	Reason    string `json:"reason"`
	Message   string `json:"message"`
	Count     int32  `json:"count"`
	FirstSeen string `json:"firstSeen,omitempty"`
	LastSeen  string `json:"lastSeen,omitempty"`
	Source    string `json:"source,omitempty"`
}

// listObjectEvents serves the Events whose involvedObject is the named object
// of this kind, newest first. Filtered here rather than with an
// involvedObject field selector: the namespace only ever holds this
// install's own objects and Events expire after the API server's event TTL
// (an hour by default), so the list stays small, and it keeps the handler
// working against clients without field-selector support.
func (s *Server) listObjectEvents(k resourceKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		var list corev1.EventList
		if err := s.client.List(r.Context(), &list, client.InNamespace(s.cfg.Namespace)); err != nil {
			writeAPIError(w, r, err)
			return
		}
		type sortable struct {
			event ObjectEvent
			last  time.Time
		}
		matched := []sortable{}
		for i := range list.Items {
			ev := &list.Items[i]
			if ev.InvolvedObject.Kind != k.kind || ev.InvolvedObject.Name != name {
				continue
			}
			first, last := eventTimes(ev)
			matched = append(matched, sortable{
				event: ObjectEvent{
					Type:      ev.Type,
					Reason:    ev.Reason,
					Message:   ev.Message,
					Count:     max(ev.Count, 1),
					FirstSeen: formatEventTime(first),
					LastSeen:  formatEventTime(last),
					Source:    eventSource(ev),
				},
				last: last,
			})
		}
		slices.SortStableFunc(matched, func(a, b sortable) int { return b.last.Compare(a.last) })
		items := make([]ObjectEvent, len(matched))
		for i, m := range matched {
			items[i] = m.event
		}
		writeItems(w, r, items)
	}
}

// eventTimes resolves an Event's first/last occurrence across both the
// legacy (firstTimestamp/lastTimestamp) and events.k8s.io (eventTime/series)
// fields - which ones are set depends on the recorder that wrote it.
func eventTimes(ev *corev1.Event) (first, last time.Time) {
	first = ev.FirstTimestamp.Time
	last = ev.LastTimestamp.Time
	if first.IsZero() {
		first = ev.EventTime.Time
	}
	if ev.Series != nil && !ev.Series.LastObservedTime.IsZero() {
		last = ev.Series.LastObservedTime.Time
	}
	if last.IsZero() {
		last = first
	}
	if last.IsZero() {
		last = ev.CreationTimestamp.Time
	}
	if first.IsZero() {
		first = last
	}
	return first, last
}

func formatEventTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func eventSource(ev *corev1.Event) string {
	if ev.ReportingController != "" {
		return ev.ReportingController
	}
	return ev.Source.Component
}
