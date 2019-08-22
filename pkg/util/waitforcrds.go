package util

import (
	"time"

	crdapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	crdclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	crdinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func WaitForCRDs(mgr manager.Manager, stopCh <-chan struct{}, crdTypes ...runtime.Object) error {
	crdClient, err := crdclient.NewForConfig(mgr.GetConfig())
	if err != nil {
		return err
	}
	crdInformerFactory := crdinformers.NewSharedInformerFactory(crdClient, 10*time.Hour)
	crdInformer := crdInformerFactory.Apiextensions().V1beta1().CustomResourceDefinitions().Informer()

	done := false
	doneCh := make(chan struct{})

	handler := func() {
		if allTypesAvailable(mgr, crdTypes...) && !done {
			done = true
			close(doneCh)
		}
	}

	scheme := mgr.GetScheme()
	wantedGvks := make(map[schema.GroupVersionKind]bool)
	for _, typ := range crdTypes {
		typeGvks, _, err := scheme.ObjectKinds(typ)
		if err != nil {
			return err
		}
		for _, gvk := range typeGvks {
			wantedGvks[gvk] = true
		}
	}

	crdInformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			crd := obj.(*crdapi.CustomResourceDefinition)
			for _, gvk := range possibleGvksFromCRD(crd) {
				if wantedGvks[gvk] {
					return true
				}
			}
			return false
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				handler()
			},
			UpdateFunc: func(_ interface{}, _ interface{}) {
				handler()
			},
		},
	})

	innerStopCh := make(chan struct{})
	defer close(innerStopCh)
	go crdInformer.Run(innerStopCh)

	select {
	case <-doneCh:
	case <-stopCh:
	}
	return nil
}

func possibleGvksFromCRD(crd *crdapi.CustomResourceDefinition) []schema.GroupVersionKind {
	var gvks []schema.GroupVersionKind
	for _, version := range crd.Spec.Versions {
		gvk := schema.GroupVersionKind{
			Group:   crd.Spec.Group,
			Version: version.Name,
			Kind:    crd.Spec.Names.Kind,
		}
		gvks = append(gvks, gvk)
	}
	return gvks
}

func allTypesAvailable(mgr manager.Manager, crdTypes ...runtime.Object) bool {
	for _, typ := range crdTypes {
		if _, err := mgr.GetCache().GetInformer(typ); err != nil {
			return false
		}
	}
	return true
}
