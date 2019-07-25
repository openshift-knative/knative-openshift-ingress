package common

import (
	"context"

	"github.com/openshift-knative/knative-openshift-ingress/pkg/controller/resources"
	routev1 "github.com/openshift/api/route/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/logging"
	"knative.dev/serving/pkg/apis/networking"
	networkingv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type BaseIngressReconciler struct {
	Client client.Client
}

func (r *BaseIngressReconciler) ReconcileIngress(ctx context.Context, ci networkingv1alpha1.IngressAccessor) error {
	logger := logging.FromContext(ctx)

	if ci.GetDeletionTimestamp() != nil {
		return r.reconcileDeletion(ctx, ci)
	}

	logger.Infof("Reconciling clusterIngress :%v", ci)

	exposed := ci.GetSpec().Visibility == networkingv1alpha1.IngressVisibilityExternalIP
	if exposed {
		routes, err := resources.MakeRoutes(ci)
		if err != nil {
			return err
		}

		for _, route := range routes {
			logger.Infof("Creating/Updating OpenShift Route for host %s", route.Spec.Host)
			if err := r.reconcileRoute(ctx, ci, route); err != nil {
				return err
			}
		}
	} else {
		r.deleteRoutes(ctx, ci)
	}

	logger.Info("ClusterIngress successfully synced")
	return nil
}

func (r *BaseIngressReconciler) deleteRoutes(ctx context.Context, ci networkingv1alpha1.IngressAccessor) error {
	logger := logging.FromContext(ctx)
	listOpts := &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{
			networking.IngressLabelKey: ci.GetName(),
		}),
	}
	var routeList routev1.RouteList
	if err := r.Client.List(ctx, listOpts, &routeList); err != nil {
		return err
	}

	for _, route := range routeList.Items {
		logger.Infof("Deleting OpenShift Route for host %s", route.Spec.Host)
		if err := r.Client.Delete(ctx, &route); err != nil {
			return err
		}
		logger.Infof("Deleted OpenShift Route %q in namespace %q", route.Name, route.Namespace)
	}
	return nil
}

func (r *BaseIngressReconciler) reconcileRoute(ctx context.Context, ci networkingv1alpha1.IngressAccessor, desired *routev1.Route) error {
	logger := logging.FromContext(ctx)

	// Check if this Route already exists
	route := &routev1.Route{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, route)
	if err != nil && errors.IsNotFound(err) {
		err = r.Client.Create(ctx, desired)
		if err != nil {
			logger.Errorf("Failed to create OpenShift Route %q in namespace %q: %v", desired.Name, desired.Namespace, err)
			return err
		}
		logger.Infof("Created OpenShift Route %q in namespace %q", desired.Name, desired.Namespace)
	} else if err != nil {
		return err
	} else if !equality.Semantic.DeepEqual(route.Spec, desired.Spec) {
		// Don't modify the informers copy
		existing := route.DeepCopy()
		existing.Spec = desired.Spec
		existing.Annotations = desired.Annotations
		err = r.Client.Update(ctx, existing)
		if err != nil {
			logger.Errorf("Failed to update OpenShift Route %q in namespace %q: %v", desired.Name, desired.Namespace, err)
			return err
		}
	}

	return nil
}

func (r *BaseIngressReconciler) reconcileDeletion(ctx context.Context, ci networkingv1alpha1.IngressAccessor) error {
	// TODO: something with a finalizer?  We're using owner refs for
	// now, but really shouldn't be using owner refs from
	// cluster-scoped ClusterIngress to a namespace-scoped K8s
	// Service.
	return nil
}
