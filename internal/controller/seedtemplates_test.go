package controller

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
)

func TestSeedDefaultTestCaseTemplatesResetsEditedTemplate(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := benchv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	edited := &benchv1alpha1.TestCaseTemplate{
		Name: DefaultTemplateName, Namespace: "default",
		Spec: benchv1alpha1.TestCaseTemplateSpec{Script: "edited", Description: "edited"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(edited).Build()

	if err := SeedDefaultTestCaseTemplates(t.Context(), c, "default"); err != nil {
		t.Fatal(err)
	}
	var got benchv1alpha1.TestCaseTemplate
	if err := c.Get(t.Context(), types.NamespacedName{Name: DefaultTemplateName, Namespace: "default"}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.Script != mustEmbeddedScript("login.ts") {
		t.Errorf("edited built-in template kept its script %q, want the embedded login.ts", got.Spec.Script)
	}
	var all benchv1alpha1.TestCaseTemplateList
	if err := c.List(t.Context(), &all); err != nil {
		t.Fatal(err)
	}
	if len(all.Items) != len(defaultTestCaseTemplates("default")) {
		t.Errorf("seeded %d templates, want %d", len(all.Items), len(defaultTestCaseTemplates("default")))
	}
}
