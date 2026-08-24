import { TopologyForm } from "@/components/topology-form";

export default function NewTopologyPage() {
  return (
    <>
      <h1>New topology</h1>
      <p className="subtitle">
        The operator installs the authentik chart onto the chosen cluster and reports
        readiness back here.
      </p>
      <TopologyForm />
    </>
  );
}
