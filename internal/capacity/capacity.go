// Package capacity answers whether a workload cluster can schedule a set of
// pods - an AuthentikDeployment's or a TestCase's - before they're created,
// counting the nodes cluster-autoscaler could still add. It approximates the
// scheduler with first-fit placement on resource requests plus this
// project's own node isolation rules (internal/render/isolation.go): it is
// meant to catch "this can never fit", not to predict exact placement.
package capacity

import (
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Pod is one pod to be placed.
type Pod struct {
	Name     string
	Requests corev1.ResourceList
	// Deployment is the pod's bench.goauthentik.io/deployment label.
	Deployment string
	// Isolating pods (an AuthentikDeployment's own) carry the required
	// cross-deployment anti-affinity: they never share a node with a pod
	// labeled for another deployment. Non-isolating pods (k6 runners)
	// instead avoid their own deployment's nodes - and, because required
	// anti-affinity is symmetric, every isolating pod of another deployment.
	Isolating bool
}

// Group is a set of pods placed together under one constraint.
type Group struct {
	Pods []Pod
	// Spread places every pod of the group on a different node (Dedicated).
	Spread bool
	// Packed places every pod of the group on the same node (Packed).
	Packed bool
}

// Placed is a pod already running on a node, as far as placement rules care.
type Placed struct {
	Deployment string
	Isolating  bool
}

// Node is a node's remaining room for new pods.
type Node struct {
	Name string
	Free corev1.ResourceList
	Pods []Placed
	// Virtual nodes don't exist yet: cluster-autoscaler can still add them.
	Virtual bool
}

// Pool is every node a set of pods may be scheduled onto.
type Pool struct {
	// Description names the pool in error messages, e.g. `"authentik" pool`.
	Description string
	Nodes       []Node
}

// Fit places groups onto pool in order, returning a descriptive error for
// the first pod that fits nowhere. pool is not modified.
func Fit(pool Pool, groups []Group) error {
	nodes := make([]Node, len(pool.Nodes))
	for i, n := range pool.Nodes {
		nodes[i] = Node{Name: n.Name, Free: n.Free.DeepCopy(), Pods: slices.Clone(n.Pods), Virtual: n.Virtual}
	}
	for _, g := range groups {
		pods := g.Pods
		if g.Packed && len(pods) > 1 {
			pods = []Pod{packed(pods)}
		}
		// Largest first, so a big pod isn't starved by small ones.
		pods = slices.Clone(pods)
		slices.SortStableFunc(pods, func(a, b Pod) int { return compareSize(b.Requests, a.Requests) })
		used := map[int]bool{}
		for _, p := range pods {
			i := place(nodes, p, func(i int) bool { return !g.Spread || !used[i] })
			if i < 0 {
				return unschedulable(pool, nodes, p)
			}
			used[i] = true
		}
	}
	return nil
}

func place(nodes []Node, p Pod, allowed func(int) bool) int {
	need := withPodSlot(p.Requests)
	for i := range nodes {
		n := &nodes[i]
		if !allowed(i) || !compatible(n.Pods, p) || !fits(n.Free, need) {
			continue
		}
		for name, q := range need {
			free := n.Free[name]
			free.Sub(q)
			n.Free[name] = free
		}
		n.Pods = append(n.Pods, Placed{Deployment: p.Deployment, Isolating: p.Isolating})
		return i
	}
	return -1
}

// compatible applies the isolation rules in Pod's doc comment.
func compatible(existing []Placed, p Pod) bool {
	for _, e := range existing {
		if e.Deployment == "" {
			continue
		}
		if p.Isolating && e.Deployment != p.Deployment {
			return false
		}
		if !p.Isolating && (e.Deployment == p.Deployment || e.Isolating) {
			return false
		}
	}
	return true
}

func fits(free, need corev1.ResourceList) bool {
	for name, q := range need {
		f, ok := free[name]
		if !ok {
			// A resource the node doesn't report (e.g. an extended
			// resource) can't be satisfied; core resources are always set.
			if q.IsZero() {
				continue
			}
			return false
		}
		if f.Cmp(q) < 0 {
			return false
		}
	}
	return true
}

func withPodSlot(requests corev1.ResourceList) corev1.ResourceList {
	out := corev1.ResourceList{corev1.ResourcePods: resource.MustParse("1")}
	for name, q := range requests {
		if name == corev1.ResourceCPU || name == corev1.ResourceMemory || name == corev1.ResourceEphemeralStorage {
			out[name] = q.DeepCopy()
		}
	}
	return out
}

func packed(pods []Pod) Pod {
	out := Pod{Name: pods[0].Name, Requests: corev1.ResourceList{}, Deployment: pods[0].Deployment, Isolating: pods[0].Isolating}
	names := make([]string, 0, len(pods))
	for _, p := range pods {
		names = append(names, p.Name)
		for name, q := range p.Requests {
			sum := out.Requests[name]
			sum.Add(q)
			out.Requests[name] = sum
		}
	}
	out.Name = strings.Join(names, " + ") + " (packed)"
	return out
}

func compareSize(a, b corev1.ResourceList) int {
	if c := a.Cpu().Cmp(*b.Cpu()); c != 0 {
		return c
	}
	return a.Memory().Cmp(*b.Memory())
}

func unschedulable(pool Pool, nodes []Node, p Pod) error {
	existing, virtual, eligible := 0, 0, 0
	for _, n := range nodes {
		if n.Virtual {
			virtual++
		} else {
			existing++
		}
		if compatible(n.Pods, p) {
			eligible++
		}
	}
	nodeDesc := fmt.Sprintf("%d node(s)", existing)
	if virtual > 0 {
		nodeDesc += fmt.Sprintf(" plus %d cluster-autoscaler can add", virtual)
	}
	return fmt.Errorf("not enough capacity in the %s: %s (%s) fits on none of its %s - %d of them are not reserved for another deployment",
		pool.Description, p.Name, describe(p.Requests), nodeDesc, eligible)
}

func describe(requests corev1.ResourceList) string {
	cpu, mem := requests.Cpu(), requests.Memory()
	if cpu.IsZero() && mem.IsZero() {
		return "no resource requests"
	}
	return fmt.Sprintf("cpu %s, memory %s", cpu.String(), mem.String())
}
