import { Content, Title } from "@patternfly/react-core";
import { TopologyForm } from "@/components/topology-form";

export default function NewTopologyPage() {
  return (
    <>
      <Title headingLevel="h1">New topology</Title>
      <Content component="p">
        The operator installs the authentik chart onto the chosen cluster and reports
        readiness back here.
      </Content>
      <TopologyForm />
    </>
  );
}
