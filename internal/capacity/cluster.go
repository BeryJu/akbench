package capacity

import (
	"fmt"
	"maps"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
	"github.com/authentik-labs/akbench/internal/render"
)

// Cluster is a workload cluster's current state.
type Cluster struct {
	WorkloadCluster *benchv1alpha1.WorkloadCluster
	Nodes           []corev1.Node
	Pods            []corev1.Pod
	// MachineDeploymentReplicas is each pool's current size, keyed by
	// render.MachineDeploymentName - what cluster-autoscaler can still grow
	// from. A pool missing here counts at its spec.workerPools replicas.
	MachineDeploymentReplicas map[string]int32
}

// Placement is where a set of pods may go: the pool role they target plus
// any extra nodeSelector/tolerations (Dedicated mode's own).
type Placement struct {
	Role         benchv1alpha1.WorkerPoolRole
	NodeSelector map[string]string
	Tolerations  []corev1.Toleration
}

// Pool returns every node pods with the given placement may land on, with
// what's left on each once running pods are accounted for, plus one virtual
// node per machine cluster-autoscaler may still add to a matching pool.
// ignore drops pods about to be replaced (e.g. the object's own, on update).
func (c Cluster) Pool(p Placement, ignore func(*corev1.Pod) bool) Pool {
	selector, tolerations := c.poolPlacement(p.Role)
	selector = merge(selector, p.NodeSelector)
	tolerations = append(append([]corev1.Toleration{}, tolerations...), p.Tolerations...)

	byNode := map[string][]*corev1.Pod{}
	for i := range c.Pods {
		pod := &c.Pods[i]
		if pod.Spec.NodeName == "" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		if ignore != nil && ignore(pod) {
			continue
		}
		byNode[pod.Spec.NodeName] = append(byNode[pod.Spec.NodeName], pod)
	}

	pool := Pool{Description: c.describe(p.Role)}
	for i := range c.Nodes {
		node := &c.Nodes[i]
		if !schedulable(node) || !labels.SelectorFromSet(selector).Matches(labels.Set(node.Labels)) || !tolerated(node.Spec.Taints, tolerations) {
			continue
		}
		pool.Nodes = append(pool.Nodes, freeNode(node, byNode[node.Name], nil))
	}
	pool.Nodes = append(pool.Nodes, c.virtualNodes(p.Role, selector, byNode)...)
	return pool
}

// poolPlacement is render's own pool targeting for role on this cluster -
// nil/nil (any untainted node) when it has no pool for that role.
func (c Cluster) poolPlacement(role benchv1alpha1.WorkerPoolRole) (map[string]string, []corev1.Toleration) {
	switch role {
	case benchv1alpha1.WorkerPoolRoleAuthentik:
		return render.AuthentikPoolPlacement(c.WorkloadCluster)
	case benchv1alpha1.WorkerPoolRoleK6:
		return render.K6PoolPlacement(c.WorkloadCluster)
	case benchv1alpha1.WorkerPoolRoleMonitoring:
		return render.MonitoringPoolPlacement(c.WorkloadCluster)
	}
	return nil, nil
}

func (c Cluster) describe(role benchv1alpha1.WorkerPoolRole) string {
	if c.WorkloadCluster.HasPoolRole(role) {
		return fmt.Sprintf("%s pool(s) of cluster %q", role, c.WorkloadCluster.Name)
	}
	return fmt.Sprintf("untainted nodes of cluster %q", c.WorkloadCluster.Name)
}

// virtualNodes models the machines cluster-autoscaler can still add to each
// autoscaled pool whose nodes these pods would target.
func (c Cluster) virtualNodes(role benchv1alpha1.WorkerPoolRole, selector map[string]string, byNode map[string][]*corev1.Pod) []Node {
	wc := c.WorkloadCluster
	usesRolePool := wc.HasPoolRole(role)
	var out []Node
	for _, pool := range wc.Spec.WorkerPools {
		if pool.Autoscaling == nil {
			continue
		}
		poolRole := pool.Role
		if poolRole == "" {
			poolRole = benchv1alpha1.WorkerPoolRoleGeneric
		}
		if usesRolePool && poolRole != role || !usesRolePool && poolRole != benchv1alpha1.WorkerPoolRoleGeneric {
			continue
		}
		mdName := render.MachineDeploymentName(wc, pool)
		current, ok := c.MachineDeploymentReplicas[mdName]
		if !ok {
			current = pool.Replicas
		}
		headroom := pool.Autoscaling.MaxReplicas - current
		if headroom <= 0 {
			continue
		}
		template, ok := c.templateNode(mdName, selector, byNode, pool)
		if !ok {
			continue
		}
		for i := range headroom {
			out = append(out, Node{Name: fmt.Sprintf("%s-new-%d", mdName, i), Free: template.DeepCopy(), Virtual: true})
		}
	}
	return out
}

// templateNode sizes a node the pool doesn't have yet: like one of its
// existing nodes (CAPI names a pool's machines, and so their nodes,
// "<machinedeployment>-..."), else any node it would share a role with,
// else its Incus flavor. Only DaemonSet pods are carried over, since they'd
// land on a new node too.
func (c Cluster) templateNode(mdName string, selector map[string]string, byNode map[string][]*corev1.Pod, pool benchv1alpha1.WorkerPoolSpec) (corev1.ResourceList, bool) {
	var fallback *corev1.Node
	for i := range c.Nodes {
		node := &c.Nodes[i]
		if strings.HasPrefix(node.Name, mdName+"-") {
			return freeNode(node, byNode[node.Name], isDaemonSetPod).Free, true
		}
		if fallback == nil && len(selector) > 0 && labels.SelectorFromSet(selector).Matches(labels.Set(node.Labels)) {
			fallback = node
		}
	}
	if fallback != nil {
		return freeNode(fallback, byNode[fallback.Name], isDaemonSetPod).Free, true
	}
	return FlavorResources(pool.Incus.Flavor)
}

var flavorPattern = regexp.MustCompile(`^c(\d+)-m(\d+)$`)

// FlavorResources is what an Incus "c<cpu>-m<memory GiB>" flavor offers,
// before any system reservation.
func FlavorResources(flavor string) (corev1.ResourceList, bool) {
	m := flavorPattern.FindStringSubmatch(flavor)
	if m == nil {
		return nil, false
	}
	cpu, _ := strconv.Atoi(m[1])
	mem, _ := strconv.Atoi(m[2])
	return corev1.ResourceList{
		corev1.ResourceCPU:    *resource.NewQuantity(int64(cpu), resource.DecimalSI),
		corev1.ResourceMemory: *resource.NewQuantity(int64(mem)<<30, resource.BinarySI),
		corev1.ResourcePods:   resource.MustParse("110"),
	}, true
}

// freeNode is node's allocatable minus the requests of its pods (only those
// keep accepts, when keep is set).
func freeNode(node *corev1.Node, pods []*corev1.Pod, keep func(*corev1.Pod) bool) Node {
	free := corev1.ResourceList{}
	for _, name := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory, corev1.ResourceEphemeralStorage, corev1.ResourcePods} {
		if q, ok := node.Status.Allocatable[name]; ok {
			free[name] = q.DeepCopy()
		}
	}
	out := Node{Name: node.Name}
	for _, pod := range pods {
		if keep != nil && !keep(pod) {
			continue
		}
		for name, q := range withPodSlot(PodRequests(pod)) {
			if f, ok := free[name]; ok {
				f.Sub(q)
				free[name] = f
			}
		}
		if dep := pod.Labels[benchv1alpha1.LabelDeployment]; dep != "" {
			out.Pods = append(out.Pods, Placed{Deployment: dep, Isolating: pod.Labels[benchv1alpha1.LabelTestCase] == ""})
		}
	}
	out.Free = free
	return out
}

// PodRequests is what the scheduler reserves for pod: its containers'
// requests summed, or its largest init container's if that's bigger, plus
// pod overhead.
func PodRequests(pod *corev1.Pod) corev1.ResourceList {
	return requests(pod.Spec)
}

// TemplateRequests is PodRequests for a pod that doesn't exist yet.
func TemplateRequests(spec corev1.PodSpec) corev1.ResourceList {
	return requests(spec)
}

func requests(spec corev1.PodSpec) corev1.ResourceList {
	out := corev1.ResourceList{}
	for _, c := range spec.Containers {
		addTo(out, c.Resources.Requests)
	}
	for _, c := range spec.InitContainers {
		for name, q := range c.Resources.Requests {
			if cur, ok := out[name]; !ok || q.Cmp(cur) > 0 {
				out[name] = q.DeepCopy()
			}
		}
	}
	addTo(out, spec.Overhead)
	return out
}

func addTo(dst, src corev1.ResourceList) {
	for name, q := range src {
		sum := dst[name]
		sum.Add(q)
		dst[name] = sum
	}
}

func isDaemonSetPod(pod *corev1.Pod) bool {
	for _, ref := range pod.OwnerReferences {
		if ref.Kind == "DaemonSet" {
			return true
		}
	}
	return false
}

func schedulable(node *corev1.Node) bool {
	if node.Spec.Unschedulable {
		return false
	}
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

func tolerated(taints []corev1.Taint, tolerations []corev1.Toleration) bool {
	for i := range taints {
		t := &taints[i]
		if t.Effect == corev1.TaintEffectPreferNoSchedule {
			continue
		}
		ok := false
		for j := range tolerations {
			if tolerations[j].ToleratesTaint(logr.Discard(), t, false) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func merge(a, b map[string]string) map[string]string {
	out := map[string]string{}
	maps.Copy(out, a)
	maps.Copy(out, b)
	return out
}
