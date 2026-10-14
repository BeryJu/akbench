package capacity

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
	"github.com/authentik-labs/akbench/internal/render"
)

func res(cpu, mem string) corev1.ResourceList {
	return corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu), corev1.ResourceMemory: resource.MustParse(mem)}
}

func node(name, cpu, mem string, placed ...Placed) Node {
	free := res(cpu, mem)
	free[corev1.ResourcePods] = resource.MustParse("110")
	return Node{Name: name, Free: free, Pods: placed}
}

func authentikPods(n int, cpu, mem string) []Pod {
	pods := make([]Pod, n)
	for i := range pods {
		pods[i] = Pod{Name: "server", Requests: res(cpu, mem), Deployment: "ak", Isolating: true}
	}
	return pods
}

func TestFitResources(t *testing.T) {
	pool := Pool{Description: "test pool", Nodes: []Node{node("a", "2", "4Gi"), node("b", "2", "4Gi")}}
	if err := Fit(pool, []Group{{Pods: authentikPods(2, "2", "2Gi")}}); err != nil {
		t.Fatalf("two 2-cpu pods on two 2-cpu nodes: %v", err)
	}
	err := Fit(pool, []Group{{Pods: authentikPods(3, "2", "2Gi")}})
	if err == nil || !strings.Contains(err.Error(), "not enough capacity in the test pool") {
		t.Fatalf("three 2-cpu pods on two 2-cpu nodes: got %v", err)
	}
	if pool.Nodes[0].Free.Cpu().String() != "2" {
		t.Error("Fit must not modify the pool it was given")
	}
}

func TestFitSpreadAndPacked(t *testing.T) {
	pool := Pool{Nodes: []Node{node("a", "8", "16Gi"), node("b", "8", "16Gi")}}
	if err := Fit(pool, []Group{{Pods: authentikPods(3, "1", "1Gi"), Spread: true}}); err == nil {
		t.Error("Dedicated: three pods need three nodes")
	}
	if err := Fit(pool, []Group{{Pods: authentikPods(2, "1", "1Gi"), Spread: true}}); err != nil {
		t.Errorf("Dedicated: two pods on two nodes: %v", err)
	}
	if err := Fit(pool, []Group{{Pods: authentikPods(3, "3", "1Gi"), Packed: true}}); err == nil {
		t.Error("Packed: 9 cpu can't share one 8-cpu node")
	}
	if err := Fit(pool, []Group{{Pods: authentikPods(2, "3", "1Gi"), Packed: true}}); err != nil {
		t.Errorf("Packed: 6 cpu on one 8-cpu node: %v", err)
	}
}

func TestFitIsolation(t *testing.T) {
	other := Placed{Deployment: "other", Isolating: true}
	own := Placed{Deployment: "ak", Isolating: true}
	otherK6 := Placed{Deployment: "other", Isolating: false}

	// A node running another deployment's pods is off limits, whatever room it has.
	pool := Pool{Nodes: []Node{node("a", "8", "16Gi", other)}}
	if err := Fit(pool, []Group{{Pods: authentikPods(1, "100m", "64Mi")}}); err == nil {
		t.Error("authentik pod must not share a node with another deployment")
	}
	pool = Pool{Nodes: []Node{node("a", "8", "16Gi", own)}}
	if err := Fit(pool, []Group{{Pods: authentikPods(1, "100m", "64Mi")}}); err != nil {
		t.Errorf("authentik pod may join its own deployment's node: %v", err)
	}
	// ...including a node running a k6 runner aimed at another deployment.
	pool = Pool{Nodes: []Node{node("a", "8", "16Gi", otherK6)}}
	if err := Fit(pool, []Group{{Pods: authentikPods(1, "100m", "64Mi")}}); err == nil {
		t.Error("authentik pod must not share a node with another deployment's k6 runner")
	}

	k6 := Pod{Name: "k6", Requests: res("1", "512Mi"), Deployment: "ak"}
	for name, placed := range map[string]Placed{"its target's": own, "another deployment's": other} {
		pool = Pool{Nodes: []Node{node("a", "8", "16Gi", placed)}}
		if err := Fit(pool, []Group{{Pods: []Pod{k6}}}); err == nil {
			t.Errorf("k6 pod must not share a node with %s pods", name)
		}
	}
	pool = Pool{Nodes: []Node{node("a", "8", "16Gi", otherK6)}}
	if err := Fit(pool, []Group{{Pods: []Pod{k6}}}); err != nil {
		t.Errorf("k6 pods for different targets may share a node: %v", err)
	}
	pool = Pool{Nodes: []Node{node("a", "8", "16Gi", Placed{Deployment: "ak"})}}
	if err := Fit(pool, []Group{{Pods: []Pod{k6}}}); err == nil {
		t.Error("two k6 pods for the same target must not share a node")
	}
}

func readyNode(name string, labels map[string]string, taints []corev1.Taint, cpu, mem string) corev1.Node {
	alloc := res(cpu, mem)
	alloc[corev1.ResourcePods] = resource.MustParse("110")
	return corev1.Node{
		Name: name, Labels: labels,
		Spec: corev1.NodeSpec{Taints: taints},
		Status: corev1.NodeStatus{
			Allocatable: alloc,
			Conditions:  []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
		},
	}
}

func runningPod(name, nodeName string, labels map[string]string, cpu, mem string, daemonSet bool) corev1.Pod {
	pod := corev1.Pod{
		Name: name, Labels: labels,
		Spec: corev1.PodSpec{
			NodeName:   nodeName,
			Containers: []corev1.Container{{Name: "c", Resources: corev1.ResourceRequirements{Requests: res(cpu, mem)}}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	if daemonSet {
		pod.OwnerReferences = []metav1.OwnerReference{{Kind: "DaemonSet", Name: "ds"}}
	}
	return pod
}

func TestClusterPool(t *testing.T) {
	wc := &benchv1alpha1.WorkloadCluster{
		Name: "wc",
		Spec: benchv1alpha1.WorkloadClusterSpec{WorkerPools: []benchv1alpha1.WorkerPoolSpec{
			{Name: "authentik", Role: benchv1alpha1.WorkerPoolRoleAuthentik, Replicas: 1,
				Incus: benchv1alpha1.IncusMachineSpec{Flavor: "c4-m8"}, Autoscaling: &benchv1alpha1.AutoscalingSpec{MaxReplicas: 3}},
			{Name: "k6", Role: benchv1alpha1.WorkerPoolRoleK6, Replicas: 1,
				Incus: benchv1alpha1.IncusMachineSpec{Flavor: "c2-m4"}, Autoscaling: &benchv1alpha1.AutoscalingSpec{MaxReplicas: 2}},
		}},
	}
	authentikLabel := map[string]string{render.NodePoolLabel: "authentik"}
	authentikTaint := []corev1.Taint{{Key: render.NodePoolLabel, Value: "authentik", Effect: corev1.TaintEffectNoSchedule}}
	k6Taint := []corev1.Taint{{Key: render.NodePoolLabel, Value: "k6", Effect: corev1.TaintEffectNoSchedule}}
	cordoned := readyNode("wc-authentik-cordoned", authentikLabel, authentikTaint, "4", "8Gi")
	cordoned.Spec.Unschedulable = true

	c := Cluster{
		WorkloadCluster: wc,
		Nodes: []corev1.Node{
			readyNode("wc-authentik-abc-1", authentikLabel, authentikTaint, "4", "8Gi"),
			cordoned,
			readyNode("wc-k6-abc-1", map[string]string{render.NodePoolLabel: "k6"}, k6Taint, "2", "4Gi"),
		},
		Pods: []corev1.Pod{
			runningPod("ds", "wc-authentik-abc-1", nil, "500m", "512Mi", true),
			runningPod("own", "wc-authentik-abc-1", map[string]string{benchv1alpha1.LabelDeployment: "ak"}, "1", "1Gi", false),
		},
		MachineDeploymentReplicas: map[string]int32{"wc-authentik": 1},
	}

	pool := c.Pool(Placement{Role: benchv1alpha1.WorkerPoolRoleAuthentik}, nil)
	var existing, virtual []Node
	for _, n := range pool.Nodes {
		if n.Virtual {
			virtual = append(virtual, n)
		} else {
			existing = append(existing, n)
		}
	}
	if len(existing) != 1 || existing[0].Name != "wc-authentik-abc-1" {
		t.Fatalf("expected only the ready, uncordoned authentik node, got %+v", existing)
	}
	if got := existing[0].Free.Cpu().String(); got != "2500m" {
		t.Errorf("free cpu = %s, want 2500m (4 - 500m daemonset - 1 own pod)", got)
	}
	if len(virtual) != 2 {
		t.Fatalf("expected 2 autoscaler nodes (max 3, 1 running), got %d", len(virtual))
	}
	if got := virtual[0].Free.Cpu().String(); got != "3500m" {
		t.Errorf("virtual node cpu = %s, want 3500m (an existing node minus its daemonset pods)", got)
	}

	// Ignoring the deployment's own pods frees their room and their claim on the node.
	pool = c.Pool(Placement{Role: benchv1alpha1.WorkerPoolRoleAuthentik}, func(p *corev1.Pod) bool {
		return p.Labels[benchv1alpha1.LabelDeployment] == "ak"
	})
	if got := pool.Nodes[0].Free.Cpu().String(); got != "3500m" || len(pool.Nodes[0].Pods) != 0 {
		t.Errorf("ignoring own pods: free cpu %s, placed %v", got, pool.Nodes[0].Pods)
	}

	// No k6 node existing in a role-less way falls back on the matching node's size.
	pool = c.Pool(Placement{Role: benchv1alpha1.WorkerPoolRoleK6}, nil)
	if len(pool.Nodes) != 2 || pool.Nodes[0].Name != "wc-k6-abc-1" || !pool.Nodes[1].Virtual {
		t.Fatalf("k6 pool: %+v", pool.Nodes)
	}

	// 3 authentik nodes' worth of Dedicated pods fits only thanks to the autoscaler.
	pods := make([]Pod, 0, 4)
	pods = append(pods, authentikPods(3, "2", "2Gi")...)
	if err := Fit(c.Pool(Placement{Role: benchv1alpha1.WorkerPoolRoleAuthentik}, nil), []Group{{Pods: pods, Spread: true}}); err != nil {
		t.Errorf("fits with autoscaling: %v", err)
	}
	pods = append(pods, pods[0])
	if err := Fit(c.Pool(Placement{Role: benchv1alpha1.WorkerPoolRoleAuthentik}, nil), []Group{{Pods: pods, Spread: true}}); err == nil {
		t.Error("4 dedicated pods can't fit on at most 3 nodes")
	}
}

func TestVirtualNodeFromFlavor(t *testing.T) {
	wc := &benchv1alpha1.WorkloadCluster{
		Name: "wc",
		Spec: benchv1alpha1.WorkloadClusterSpec{WorkerPools: []benchv1alpha1.WorkerPoolSpec{
			{Name: "authentik", Role: benchv1alpha1.WorkerPoolRoleAuthentik, Replicas: 0,
				Incus: benchv1alpha1.IncusMachineSpec{Flavor: "c4-m8"}, Autoscaling: &benchv1alpha1.AutoscalingSpec{MaxReplicas: 1}},
		}},
	}
	pool := Cluster{WorkloadCluster: wc}.Pool(Placement{Role: benchv1alpha1.WorkerPoolRoleAuthentik}, nil)
	if len(pool.Nodes) != 1 || pool.Nodes[0].Free.Cpu().String() != "4" || pool.Nodes[0].Free.Memory().String() != "8Gi" {
		t.Fatalf("pool scaled to zero should be sized by its flavor: %+v", pool.Nodes)
	}
}

func TestPodRequests(t *testing.T) {
	pod := &corev1.Pod{Spec: corev1.PodSpec{
		Containers: []corev1.Container{
			{Resources: corev1.ResourceRequirements{Requests: res("1", "1Gi")}},
			{Resources: corev1.ResourceRequirements{Requests: res("500m", "1Gi")}},
		},
		InitContainers: []corev1.Container{{Resources: corev1.ResourceRequirements{Requests: res("2", "512Mi")}}},
	}}
	got := PodRequests(pod)
	if got.Cpu().String() != "2" || got.Memory().String() != "2Gi" {
		t.Errorf("PodRequests = cpu %s memory %s, want cpu 2 (init) memory 2Gi (containers)", got.Cpu(), got.Memory())
	}
}
