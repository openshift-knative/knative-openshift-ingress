package util

import (
	"time"

	crdapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	crdclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	crdinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func WaitForCRDs(mgr manager.Manager, stopCh <-chan struct{}, crdNameTypes map[string]runtime.Object) error {
	crdClient, err := crdclient.NewForConfig(mgr.GetConfig())
	if err != nil {
		return err
	}
	crdInformerFactory := crdinformers.NewSharedInformerFactory(crdClient, 10*time.Hour)
	crdInformer := crdInformerFactory.Apiextensions().V1beta1().CustomResourceDefinitions().Informer()

	done := false
	doneCh := make(chan struct{})

	handler := func() {
		if allTypesAvailable(mgr, crdNameTypes) && !done {
			done = true
			close(doneCh)
		}
	}

	crdInformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			crd := obj.(*crdapi.CustomResourceDefinition)
			// Filtering by name because trying out on all CRDs is super expensive.
			_, ok := crdNameTypes[crd.Name]
			return ok
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

func allTypesAvailable(mgr manager.Manager, crdNameTypes map[string]runtime.Object) bool {
	for _, typ := range crdNameTypes {
		if _, err := mgr.GetCache().GetInformer(typ); err != nil {
			return false
		}
	}
	return true
}
