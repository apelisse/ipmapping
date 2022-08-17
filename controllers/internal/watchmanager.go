/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// WatchManager is a dynamic watcher you can add and remove watches
// dynamically.
type WatchManager interface {
	// Watch creates and starts a new watch for the given GVR. If
	// the watch can't be started, an error is returned. The watch
	// can be stopped by calling its Stop method.
	Watch(gvr schema.GroupVersionResource, namespace string, handler func(lister cache.GenericNamespaceLister, name string) error) (Watch, error)
}

type watchManager struct {
	client dynamic.Interface
	log    logr.Logger
}

// NewWatchManager creates a new watch factory that can watch arbitrary
// resources, and the watches can be stopped. This automatically uses
// the in-cluster configuration.
func NewWatchManager(config *rest.Config, log logr.Logger) (WatchManager, error) {
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &watchManager{client: client, log: log.WithName("WatchManager")}, nil
}

// Watch implements the WatchManager interface.
func (w *watchManager) Watch(gvr schema.GroupVersionResource, namespace string, handler func(lister cache.GenericNamespaceLister, key string) error) (Watch, error) {
	w.log.Info("Adding new watch", "gvr", gvr, "namespace", namespace)
	informer := dynamicinformer.NewFilteredDynamicInformer(w.client, gvr, namespace, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, nil)
	lister := informer.Lister().ByNamespace(namespace)
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	actualInformer := informer.Informer()
	actualInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err != nil {
				w.log.Error(err, "Failed to get key")
				return
			}
			_, name, err := cache.SplitMetaNamespaceKey(key)
			if err != nil {
				w.log.Error(err, "Failed to split namespace", "key", key)
				return
			}
			queue.Add(name)
		},
		UpdateFunc: func(_ interface{}, newObj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(newObj)
			if err != nil {
				w.log.Error(err, "Failed to get key")
				return
			}
			_, name, err := cache.SplitMetaNamespaceKey(key)
			if err != nil {
				w.log.Error(err, "Failed to split namespace", "key", key)
				return
			}
			queue.Add(name)
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err != nil {
				w.log.Error(err, "Failed to get key")
				return
			}
			_, name, err := cache.SplitMetaNamespaceKey(key)
			if err != nil {
				w.log.Error(err, "Failed to split namespace", "key", key)
				return
			}
			queue.Add(name)
		},
	})

	watch := &watch{
		workerStopCh:   make(chan struct{}),
		informerStopCh: make(chan struct{}),
		lister:         lister,
		queue:          queue,
		handler:        handler,
		log:            w.log.WithName("Watch"),
		namespace:      namespace,
		gvr:            gvr,
	}

	go func() {
		actualInformer.Run(watch.informerStopCh)
	}()

	go func() {
		// wait for the caches to synchronize before starting the worker
		if !cache.WaitForCacheSync(watch.workerStopCh, actualInformer.HasSynced) {
			utilruntime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
			return
		}
		w.log.Info("Worker started", "gvr", gvr, "namespace", namespace)
		wait.Until(watch.runWorker, time.Second, watch.workerStopCh)
	}()

	return watch, nil
}

type Watch interface {
	Stop()
}

type watch struct {
	workerStopCh   chan struct{}
	informerStopCh chan struct{}
	lister         cache.GenericNamespaceLister
	queue          workqueue.RateLimitingInterface
	handler        func(lister cache.GenericNamespaceLister, name string) error
	log            logr.Logger
	gvr            schema.GroupVersionResource
	namespace      string
}

func (w *watch) runWorker() {
	for {
		key, quit := w.queue.Get()
		if quit {
			return
		}

		err := w.handler(w.lister, key.(string))
		if err == nil {
			w.queue.Forget(key)
		} else {
			w.queue.AddRateLimited(key)
		}
		w.queue.Done(key)
	}
}

// Stop terminates the current watch and returns the associated resources.
func (w *watch) Stop() {
	w.log.Info("Terminating watch", "gvr", w.gvr, "namespace", w.namespace)
	w.queue.ShutDown()
	close(w.informerStopCh)
	w.workerStopCh <- struct{}{}
}
