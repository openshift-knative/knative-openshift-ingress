package v1alpha1

import (
	"github.com/openshift-knative/knative-openshift-ingress/pkg/apis"
	networkingv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
)

func init() {
	apis.AddToSchemes = append(apis.AddToSchemes, networkingv1alpha1.SchemeBuilder.AddToScheme)
}
