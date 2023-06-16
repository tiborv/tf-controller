package polling

import (
	"context"
	"fmt"

	"github.com/fluxcd/pkg/runtime/acl"
	sourcev1b2 "github.com/fluxcd/source-controller/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/weaveworks/tf-controller/api/v1alpha2"
)

func (s *Server) getTerraform(ctx context.Context, ref client.ObjectKey) (*v1alpha2.Terraform, error) {
	obj := &v1alpha2.Terraform{}
	err := s.clusterClient.Get(ctx, ref, obj)
	if err != nil {
		return nil, fmt.Errorf("unable to get Terraform: %w", err)
	}

	return obj, nil
}

func (s *Server) getSource(ctx context.Context, tf *v1alpha2.Terraform) (*sourcev1b2.GitRepository, error) {
	if tf.Spec.SourceRef.Kind != sourcev1b2.GitRepositoryKind {
		return nil, fmt.Errorf("branch based planner does not support source kind: %s", tf.Spec.SourceRef.Kind)
	}

	ref := client.ObjectKey{
		Namespace: tf.GetNamespace(),
		Name:      tf.Spec.SourceRef.Name,
	}
	if ns := tf.Spec.SourceRef.Namespace; ns != "" {
		ref.Namespace = ns
	}

	if s.noCrossNamespaceRefs && ref.Namespace != tf.GetNamespace() {
		return nil, acl.AccessDeniedError(
			fmt.Sprintf("cannot access %s/%s, cross-namespace references have been disabled", tf.Spec.SourceRef.Kind, ref),
		)
	}

	obj := &sourcev1b2.GitRepository{}
	err := s.clusterClient.Get(ctx, ref, obj)
	if err != nil {
		return nil, fmt.Errorf("unable to get Source: %w", err)
	}

	return obj, nil
}

func (s *Server) getSecret(ctx context.Context, ref client.ObjectKey) (*corev1.Secret, error) {
	obj := &corev1.Secret{}
	err := s.clusterClient.Get(ctx, ref, obj)
	if err != nil {
		return nil, fmt.Errorf("unable to get Secret: %w", err)
	}

	return obj, nil
}
