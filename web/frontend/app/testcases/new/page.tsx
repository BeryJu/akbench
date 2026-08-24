import { TestCaseForm } from "@/components/testcase-form";

export default function NewTestCasePage() {
  return (
    <>
      <h1>New test case</h1>
      <p className="subtitle">
        The name becomes the k6 <span className="mono">testid</span> tag, so pick one you
        will recognise on a dashboard months from now.
      </p>
      <TestCaseForm />
    </>
  );
}
