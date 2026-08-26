import { Content, Title } from "@patternfly/react-core";
import { TestCaseForm } from "@/components/testcase-form";

export default function NewTestCasePage() {
  return (
    <>
      <Title headingLevel="h1">New test case</Title>
      <Content component="p">
        The name becomes the k6 <code>testid</code> tag, so pick one you will recognise on a
        dashboard months from now.
      </Content>
      <TestCaseForm />
    </>
  );
}
