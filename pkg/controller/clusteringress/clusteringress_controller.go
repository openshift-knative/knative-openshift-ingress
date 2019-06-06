package clusteringress

import (
	"context"
	"reflect"

	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/apis/networking"
	networkingv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/openshift-knative/knative-openshift-ingress/pkg/controller/clusteringress/resources"
	routev1 "github.com/openshift/api/route/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// Add creates a new ClusterIngress Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileClusterIngress{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("clusteringress-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource ClusterIngress
	err = c.Watch(&source.Kind{Type: &networkingv1alpha1.ClusterIngress{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secondary resource Routes and requeue the
	// owner ClusterIngress
	err = c.Watch(&source.Kind{Type: &routev1.Route{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &networkingv1alpha1.ClusterIngress{},
	})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileClusterIngress{}

// ReconcileClusterIngress reconciles a ClusterIngress object
type ReconcileClusterIngress struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a ClusterIngress
// object and makes changes based on the state read and what is in the
// ClusterIngress.Spec
//
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileClusterIngress) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	ctx := context.TODO()
	logger := logging.FromContext(ctx)

	// Fetch the ClusterIngress instance
	original := &networkingv1alpha1.ClusterIngress{}
	err := r.client.Get(context.TODO(), request.NamespacedName, original)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Don't modify the informer's copy
	ci := original.DeepCopy()
	err = r.reconcile(ctx, ci)
	if equality.Semantic.DeepEqual(original.Status, ci.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else if _, err := r.updateStatus(ctx, ci); err != nil {
		logger.Warnw("Failed to update clusterIngress status", err)
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// Update the Status of the ClusterIngress.  Caller is responsible for checking
// for semantic differences before calling.
func (r *ReconcileClusterIngress) updateStatus(ctx context.Context, desired *networkingv1alpha1.ClusterIngress) (*networkingv1alpha1.ClusterIngress, error) {
	ci := &networkingv1alpha1.ClusterIngress{}
	err := r.client.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, ci)
	if err != nil {
		return nil, err
	}

	// If there's nothing to update, just return.
	if reflect.DeepEqual(ci.Status, desired.Status) {
		return ci, nil
	}
	// Don't modify the informers copy
	existing := ci.DeepCopy()
	existing.Status = desired.Status
	err = r.client.Status().Update(ctx, existing)
	return existing, err
}

func (r *ReconcileClusterIngress) reconcile(ctx context.Context, ci *networkingv1alpha1.ClusterIngress) error {
	logger := logging.FromContext(ctx)

	if ci.GetDeletionTimestamp() != nil {
		return r.reconcileDeletion(ctx, ci)
	}

	logger.Infof("Reconciling clusterIngress :%v", ci)

	exposed := ci.Spec.Visibility == networkingv1alpha1.IngressVisibilityExternalIP
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

func (r *ReconcileClusterIngress) deleteRoutes(ctx context.Context, ci *networkingv1alpha1.ClusterIngress) error {
	logger := logging.FromContext(ctx)
	listOpts := &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{
			networking.IngressLabelKey: ci.Name,
		}),
	}
	var routeList routev1.RouteList
	if err := r.client.List(ctx, listOpts, &routeList); err != nil {
		return err
	}

	for _, route := range routeList.Items {
		logger.Infof("Deleting OpenShift Route for host %s", route.Spec.Host)
		if err := r.client.Delete(ctx, &route); err != nil {
			return err
		}
		logger.Infof("Deleted OpenShift Route %q in namespace %q", route.Name, route.Namespace)
	}
	return nil
}

func (r *ReconcileClusterIngress) reconcileRoute(ctx context.Context, ci *networkingv1alpha1.ClusterIngress, desired *routev1.Route) error {
	logger := logging.FromContext(ctx)

	// Check if this Route already exists
	route := &routev1.Route{}
	err := r.client.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, route)
	if err != nil && errors.IsNotFound(err) {
		err = r.client.Create(ctx, desired)
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
		err = r.client.Update(ctx, existing)
		if err != nil {
			logger.Errorf("Failed to update OpenShift Route %q in namespace %q: %v", desired.Name, desired.Namespace, err)
			return err
		}
	}

	return nil
}

func (r *ReconcileClusterIngress) reconcileDeletion(ctx context.Context, ci *networkingv1alpha1.ClusterIngress) error {
	// TODO: something with a finalizer?  We're using owner refs for
	// now, but really shouldn't be using owner refs from
	// cluster-scoped ClusterIngress to a namespace-scoped K8s
	// Service.
	return nil
}
