// Package v1alpha1 contains the Kubernetes API types Atlantis uses to store
// shared state when running with --locking-db-type=kubernetes.
// +kubebuilder:object:generate=true
// +groupName=atlantis.runatlantis.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is the API group and version of Atlantis resources.
	GroupVersion = schema.GroupVersion{Group: "atlantis.runatlantis.io", Version: "v1alpha1"}

	// SchemeBuilder registers the Atlantis types with a scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the Atlantis types to a scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
