import { ClusterForm } from "@/components/cluster-form";

export default function NewClusterPage() {
  return (
    <>
      <h1>Register cluster</h1>
      <p className="subtitle">
        Provisions a real Kubernetes cluster via Cluster API. Topologies can then target it
        by picking it from the cluster list instead of the management cluster.
      </p>
      <ClusterForm />
    </>
  );
}
