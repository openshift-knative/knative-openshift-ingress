package common

import (
	"context"
	"fmt"
	"strings"

	maistrav1 "github.com/maistra/istio-operator/pkg/apis/maistra/v1"
	"github.com/openshift-knative/knative-openshift-ingress/pkg/controller/resources"
	routev1 "github.com/openshift/api/route/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/logging"
	"knative.dev/serving/pkg/apis/networking"
	networkingv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/network"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type BaseIngressReconciler struct {
	Client client.Client
}

const (
	smmrName      = "default"
	smmrNamespace = "knative-serving-ingress"
)

func (r *BaseIngressReconciler) ReconcileIngress(ctx context.Context, ci networkingv1alpha1.IngressAccessor) error {
	logger := logging.FromContext(ctx)

	if ci.GetDeletionTimestamp() != nil {
		return r.reconcileDeletion(ctx, ci)
	}

	logger.Infof("Reconciling clusterIngress :%v", ci)

	// Only add Istio ingress to SMMR
	if ci.GetAnnotations()[networking.IngressClassAnnotationKey] == network.IstioIngressClassName {
		if err := r.reconcileSmmr(ctx, ci); err != nil {
			return err
		}
	}
	exposed := ci.GetSpec().Visibility == networkingv1alpha1.IngressVisibilityExternalIP
	if exposed {
		ingressLabels := ci.GetLabels()
		selector := map[string]string{
			networking.IngressLabelKey:     ci.GetName(),
			serving.RouteLabelKey:          ingressLabels[serving.RouteLabelKey],
			serving.RouteNamespaceLabelKey: ingressLabels[serving.RouteNamespaceLabelKey],
		}
		listOpts := &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(selector),
		}
		var existing routev1.RouteList
		if err := r.Client.List(ctx, listOpts, &existing); err != nil {
			return err
		}
		existingMap := routeMap(existing, selector)

		routes, err := resources.MakeRoutes(ci)
		if err != nil {
			logger.Warnf("Failed to generate routes from ingress %v", err)
			// Returning nil aborts the reconcilation. It will be retriggered once the status of the ingress changes.
			return nil
		}
		for _, route := range routes {
			logger.Infof("Creating/Updating OpenShift Route for host %s", route.Spec.Host)
			if err := r.reconcileRoute(ctx, ci, route); err != nil {
				return fmt.Errorf("failed to create route for host %s: %v", route.Spec.Host, err)
			}
			delete(existingMap, route.Name)
		}
		// If routes remains in existingMap, it must be obsoleted routes. Clean them up.
		for _, rt := range existingMap {
			logger.Infof("Deleting obsoleted route for host: %s", rt.Spec.Host)
			if err := r.deleteRoute(ctx, &rt); err != nil {
				return err
			}
		}
	} else {
		if err := r.deleteRoutes(ctx, ci); err != nil {
			return err
		}
	}

	logger.Info("ClusterIngress successfully synced")
	return nil
}

func (r *BaseIngressReconciler) reconcileSmmr(ctx context.Context, ci networkingv1alpha1.IngressAccessor) error {
	logger := logging.FromContext(ctx)

	// update ServiceMeshMemberRole with the namespace info where knative routes created
	smmr := &maistrav1.ServiceMeshMemberRoll{}
	// Namespace knative-serving-ingress hardcoded for now.
	// The whole component knative-openshift-ingress is going to be moved into
	// knative-serving-networking-openshift anyway, where it will be possible to statically determine the namespace to use.
	if err := r.Client.Get(ctx, types.NamespacedName{Name: smmrName, Namespace: smmrNamespace}, smmr); err != nil {
		return err
	}
	newMembers, changed := appendIfAbsent(smmr.Spec.Members, ci.GetNamespace())
	smmr.Spec.Members = newMembers

	if changed {
		if err := r.Client.Update(ctx, smmr); err != nil {
			// ref for substring https://github.com/Maistra/istio-operator/blob/maistra-1.0/pkg/controller/servicemesh/validation/memberroll.go#L95
			if strings.Contains(err.Error(), "one or more members are already defined in another ServiceMeshMemberRoll") {
				logger.Errorf("failed to update ServiceMeshMemberRole because namespace %s is already a member of another ServiceMeshMemberRoll", ci.GetNamespace())
				return nil
			}
			return err
		}
	}
	return nil
}

func routeMap(routes routev1.RouteList, selector map[string]string) map[string]routev1.Route {
	mp := make(map[string]routev1.Route, len(routes.Items))
	for _, route := range routes.Items {
		// TODO: This routeFilter is used only for testing as fake client does not support list option
		// and we can't bump the osdk version quickly. ref:
		// https://github.com/openshift-knative/knative-openshift-ingress/pull/24#discussion_r341804021
		if routeLabelFilter(route, selector) {
			mp[route.Name] = route
		}
	}
	return mp
}

// routeLabelFilter verifies if the route has required labels.
func routeLabelFilter(route routev1.Route, selector map[string]string) bool {
	labels := route.GetLabels()
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func (r *BaseIngressReconciler) deleteRoute(ctx context.Context, route *routev1.Route) error {
	logger := logging.FromContext(ctx)
	logger.Infof("Deleting OpenShift Route for host %s", route.Spec.Host)
	if err := r.Client.Delete(ctx, route); err != nil {
		return fmt.Errorf("failed to delete obsoleted route for host %s: %v", route.Spec.Host, err)
	}
	logger.Infof("Deleted OpenShift Route %q in namespace %q", route.Name, route.Namespace)
	return nil
}

func (r *BaseIngressReconciler) deleteRoutes(ctx context.Context, ci networkingv1alpha1.IngressAccessor) error {
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
		if err := r.deleteRoute(ctx, &route); err != nil {
			return err
		}
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
	smmr := &maistrav1.ServiceMeshMemberRoll{}
	// get list of ingress object for a namespace
	ingressList := networkingv1alpha1.IngressList{}
	if err := r.Client.List(ctx, &client.ListOptions{
		Namespace: ci.GetNamespace(),
	}, &ingressList); err != nil {
		return err
	}
	// If particular namespace has only one ingress object then after deletion namespace will have empty ingress object
	// So remove namespace from SMMR
	if len(ingressList.Items) == 1 {
		if err := r.Client.Get(ctx, types.NamespacedName{Name: smmrName, Namespace: smmrNamespace}, smmr); err != nil {
			return err
		}
		for i, val := range smmr.Spec.Members {
			if val == ci.GetNamespace() {
				smmr.Spec.Members = append(smmr.Spec.Members[:i], smmr.Spec.Members[i+1:]...)
				break
			}
		}
		if err := r.Client.Update(ctx, smmr); err != nil {
			return err
		}
	}
	// remove finalyzer in order to remove the ingress object
	ci.SetFinalizers(ci.GetFinalizers()[1:])
	return r.Client.Update(ctx, ci)
}

// appendIfAbsent append namespace to member if its not exist
func appendIfAbsent(members []string, routeNamespace string) ([]string, bool) {
	for _, val := range members {
		if val == routeNamespace {
			return members, false
		}
	}
	return append(members, routeNamespace), true
}
