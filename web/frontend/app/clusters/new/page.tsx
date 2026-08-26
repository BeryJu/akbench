import { Content, Title } from "@patternfly/react-core";
import { ClusterForm } from "@/components/cluster-form";

export default function NewClusterPage() {
  return (
    <>
      <Title headingLevel="h1">Register cluster</Title>
      <Content component="p">
        Provisions a real Kubernetes cluster via Cluster API. Topologies can then target it
        by picking it from the cluster list instead of the management cluster.
      </Content>
      <ClusterForm />
    </>
  );
}
