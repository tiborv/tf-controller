package polling

import (
	"context"
	"testing"

	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	infrav1 "github.com/weaveworks/tf-controller/api/v1alpha2"
	"github.com/weaveworks/tf-controller/internal/git/provider"
)

// This checks poll can be called with a little setting-up, with no
// result expected.
func Test_poll_empty(t *testing.T) {
	g := gomega.NewWithT(t)
	ns := newNamespace(g)

	// Create a source for the Terraform object to point to
	source := &sourcev1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "original-source",
			Namespace: ns.Name,
		},
		Spec: sourcev1.GitRepositorySpec{
			URL: "https://github.com/weaveworks/tf-controller",
			Reference: &sourcev1.GitRepositoryRef{
				Branch: "main",
			},
		},
	}
	expectToSucceed(g, k8sClient.Create(context.TODO(), source))

	// Create a Terraform object to be the template.
	original := &infrav1.Terraform{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "original",
			Namespace: ns.Name,
		},
		Spec: infrav1.TerraformSpec{
			SourceRef: infrav1.CrossNamespaceSourceReference{
				Name: source.Name,
				Kind: "GitRepository",
			},
		},
	}
	expectToSucceed(g, k8sClient.Create(context.TODO(), original))

	// This fakes a provider for the server to use.
	var prs []provider.PullRequest

	// Only WithClusterClient is really needed; the unexported option
	// lets us supply the fake provider.
	server, err := New(
		WithClusterClient(k8sClient),
	)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Now we'll run `poll` to step the server once, and afterwards,
	// we should be able to see what it did.
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	expectToSucceed(g, server.reconcile(ctx, original, source, prs))

	// We expect it to have done nothing! So, check it didn't create
	// any more Terraform or source objects.
	var tfList infrav1.TerraformList
	expectToSucceed(g, k8sClient.List(context.TODO(), &tfList, &client.ListOptions{
		Namespace: ns.Name,
	}))
	expectToEqual(g, len(tfList.Items), 1) // just the original
	expectToEqual(g, tfList.Items[0].Name, original.Name)

	var srcList sourcev1.GitRepositoryList
	expectToSucceed(g, k8sClient.List(context.TODO(), &srcList, &client.ListOptions{
		Namespace: ns.Name,
	}))
	expectToEqual(g, len(srcList.Items), 1) // just `source`
	expectToEqual(g, srcList.Items[0].Name, source.Name)

	t.Cleanup(func() { expectToSucceed(g, k8sClient.Delete(context.TODO(), ns)) })
}

// This checks that branch Terraform objects are created,
// when there are open pull requests,
// updated when the original Terraform object is updated,
// and deleted when the corresponding PRs are closed.
// The original Terraform object and source should be retained.
func Test_poll_reconcile_objects(t *testing.T) {
	g := gomega.NewWithT(t)
	ns := newNamespace(g)

	// Create a source for the Terraform object to point to
	source := &sourcev1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "original-source",
			Namespace: ns.Name,
			Labels: map[string]string{
				"test-label": "123",
			},
		},
		Spec: sourcev1.GitRepositorySpec{
			URL: "https://github.com/tf-controller/helloworld",
			Reference: &sourcev1.GitRepositoryRef{
				Branch: "main",
			},
		},
	}
	expectToSucceed(g, k8sClient.Create(context.TODO(), source))

	// Create a Terraform object to be the template.
	original := &infrav1.Terraform{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "original",
			Namespace: ns.Name,
			Labels: map[string]string{
				"test-label": "abc",
			},
		},
		Spec: infrav1.TerraformSpec{
			SourceRef: infrav1.CrossNamespaceSourceReference{
				Name: source.Name,
				Kind: "GitRepository",
			},
			WriteOutputsToSecret: &infrav1.WriteOutputsToSecretSpec{
				Name: "test-secret",
			},
		},
	}
	expectToSucceed(g, k8sClient.Create(context.TODO(), original))

	// This fakes a provider for the server to use.
	repo := provider.Repository{
		Project: "fake-project",
		Org:     "fake-org",
		Name:    "fake-name",
	}
	prs := []provider.PullRequest{
		{
			Repository: repo,
			Number:     1,
			BaseBranch: "main",
			HeadBranch: "test-branch-1",
		},
		{
			Repository: repo,
			Number:     2,
			BaseBranch: "main",
			HeadBranch: "test-branch-2",
		},
		{
			Repository: repo,
			Number:     3,
			BaseBranch: "main",
			HeadBranch: "test-branch-3",
		},
	}

	// Only WithClusterClient is really needed; the unexported option
	// lets us supply the fake provider.
	server, err := New(
		WithClusterClient(k8sClient),
	)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	// Now we'll run `poll` to step the server once, and afterwards,
	// we should be able to see what it did.
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	expectToSucceed(g, server.reconcile(ctx, original, source, prs))

	// We expect the branch TF objects and corresponding sources
	// to be created for each PR
	// and the original object and source to be retained.

	// Check that the Terraform objects are created with expected fields.
	var tfList infrav1.TerraformList
	expectToSucceed(g, k8sClient.List(context.TODO(), &tfList, &client.ListOptions{
		Namespace: ns.Name,
	}))

	expectToEqual(g, len(tfList.Items), 4)
	expectToEqual(g, tfList.Items[0].Name, original.Name)
	expectToEqual(g, tfList.Items[2].Name, original.Name+"-test-branch-2-2")

	expectToEqual(g, tfList.Items[1].Spec.SourceRef.Name, "original-source-test-branch-1-1")
	expectToEqual(g, tfList.Items[1].Spec.SourceRef.Namespace, ns.Name)
	expectToEqual(g, tfList.Items[1].Spec.PlanOnly, true)
	expectToEqual(g, tfList.Items[1].Spec.StoreReadablePlan, "human")
	expectToEqual(g, tfList.Items[1].Spec.WriteOutputsToSecret.Name, "test-secret-test-branch-1-1")

	expectToEqual(g, tfList.Items[3].Labels["infra.weave.works/branch-based-planner"], "true")
	expectToEqual(g, tfList.Items[3].Labels["infra.weave.works/pr-id"], "3")
	expectToEqual(g, tfList.Items[3].Labels["test-label"], "abc")

	// Check that the Source objects are created with all expected fields.
	var srcList sourcev1.GitRepositoryList
	expectToSucceed(g, k8sClient.List(context.TODO(), &srcList, &client.ListOptions{
		Namespace: ns.Name,
	}))

	expectToEqual(g, len(srcList.Items), 4)
	expectToEqual(g, srcList.Items[0].Name, source.Name)
	expectToEqual(g, srcList.Items[2].Name, source.Name+"-test-branch-2-2")

	expectToEqual(g, srcList.Items[1].Spec.Reference.Branch, "test-branch-1")

	expectToEqual(g, srcList.Items[3].Labels["infra.weave.works/branch-based-planner"], "true")
	expectToEqual(g, srcList.Items[3].Labels["infra.weave.works/pr-id"], "3")
	expectToEqual(g, srcList.Items[3].Labels["test-label"], "123")

	// Check that branch Terraform objects are updated
	// after the original Terraform object is updated.
	original.Labels["test-label"] = "xyz"
	original.Spec.WriteOutputsToSecret.Name = "new-test-secret"

	expectToSucceed(g, k8sClient.Update(context.TODO(), original))
	expectToSucceed(g, server.reconcile(ctx, original, source, prs))

	tfList.Items = nil

	expectToSucceed(g, k8sClient.List(context.TODO(), &tfList, &client.ListOptions{
		Namespace: ns.Name,
	}))

	expectToEqual(g, tfList.Items[0].Name, original.Name)
	expectToEqual(g, tfList.Items[0].Labels["test-label"], "xyz")
	expectToEqual(g, tfList.Items[0].Spec.WriteOutputsToSecret.Name, "new-test-secret")

	expectToEqual(g, tfList.Items[2].Name, original.Name+"-test-branch-2-2")
	expectToEqual(g, tfList.Items[2].Labels["test-label"], "xyz")
	expectToEqual(g, tfList.Items[2].Spec.WriteOutputsToSecret.Name, "new-test-secret-test-branch-2-2")

	// Check that corresponding Terraform objects and Sources are deleted
	// after PRs are deleted
	// and the original Terraform object and source are retained.
	prs = prs[2:]

	expectToSucceed(g, server.reconcile(ctx, original, source, prs))

	tfList.Items = nil

	expectToSucceed(g, k8sClient.List(context.TODO(), &tfList, &client.ListOptions{
		Namespace: ns.Name,
	}))

	expectToEqual(g, len(tfList.Items), 2)
	expectToEqual(g, tfList.Items[0].Name, original.Name)
	expectToEqual(g, tfList.Items[1].Name, original.Name+"-test-branch-3-3")

	srcList.Items = nil

	expectToSucceed(g, k8sClient.List(context.TODO(), &srcList, &client.ListOptions{
		Namespace: ns.Name,
	}))

	expectToEqual(g, len(srcList.Items), 2)
	expectToEqual(g, srcList.Items[0].Name, source.Name)
	expectToEqual(g, srcList.Items[1].Name, source.Name+"-test-branch-3-3")

	t.Cleanup(func() { expectToSucceed(g, k8sClient.Delete(context.TODO(), ns)) })
}
