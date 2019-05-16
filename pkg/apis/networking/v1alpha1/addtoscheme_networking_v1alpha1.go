package v1alpha1

import (
	networkingv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/openshift-knative/knative-openshift-ingress/pkg/apis"
)

func init() {
	apis.AddToSchemes = append(apis.AddToSchemes, networkingv1alpha1.SchemeBuilder.AddToScheme)
}
