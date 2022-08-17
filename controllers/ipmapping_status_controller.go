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
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	changegroupv1beta1 "change.me.later/ipmapping/api/v1beta1"
	controllers "change.me.later/ipmapping/controllers/internal"
)

// IPMappingStatusReconciler reconciles a IPMapping status
type IPMappingStatusReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	watcher controllers.NamedWatcher
	watches map[types.NamespacedName]controllers.Watch
}

// Apply the given ipAddress to the status of the IPMapping. If the
// ipAddress pointer is nil, this will force to remove the ipMapping
// from the status, forcing the removal of the endpoint object.
func (r *IPMappingStatusReconciler) ApplyIPMappingStatus(ctx context.Context, nn types.NamespacedName, ipAddress *string) error {
	mapping := changegroupv1beta1.IPMapping{
		TypeMeta: metav1.TypeMeta{
			Kind:       "IPMapping",
			APIVersion: changegroupv1beta1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
		},
		Status: changegroupv1beta1.IPMappingStatus{
			IPAddress: ipAddress,
		},
	}
	return r.Status().Patch(ctx, &mapping, client.Merge)
}

func findField(obj runtime.Object, path *string) (*string, error) {
	if path == nil {
		return nil, nil
	}
	return ParsePath(*path).Find(obj.(*unstructured.Unstructured))
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *IPMappingStatusReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.Info("Reconciling")

	// TODO: I'm not sure this should be here (or way further, below).
	// Update the watch and configure it to update the mapping status.
	if watch, ok := r.watches[req.NamespacedName]; ok {
		log.Info("Deleting watch")
		watch.Stop()
		delete(r.watches, req.NamespacedName)
	}

	// Object is gone, forget about it.
	var ipMapping changegroupv1beta1.IPMapping
	if err := r.Get(ctx, req.NamespacedName, &ipMapping); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// TODO: This shouldn't use the unsafe form.
	gvr, _ := meta.UnsafeGuessKindToResource(schema.FromAPIVersionAndKind(ipMapping.Spec.ObjectRef.APIVersion, ipMapping.Spec.ObjectRef.Kind))
	watch, err := r.watcher.Watch(gvr, req.Namespace, ipMapping.Spec.ObjectRef.Name, func(lister cache.GenericNamespaceLister) error {
		name := ipMapping.Spec.ObjectRef.Name
		log := log.WithName("WatchHandler")
		log.Info("Event received", "gvr", gvr, "namespace", req.Namespace, "name", name)
		obj, err := lister.Get(name)
		var ipAddress *string
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
			log.Info("Target deleted", "gvr", gvr, "namespace", req.Namespace, "name", name)
		} else {
			ipAddress, err = findField(obj, ipMapping.Spec.IPPath)
			if err != nil {
				return err
			}
		}
		return r.ApplyIPMappingStatus(ctx, req.NamespacedName, ipAddress)
	})
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("Failed to create watch: %v", err)
	}
	r.watches[req.NamespacedName] = watch

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *IPMappingStatusReconciler) SetupWithManager(mgr ctrl.Manager, log logr.Logger) error {
	watchMgr, err := controllers.NewWatchManager(log)
	if err != nil {
		return err
	}
	r.watcher = controllers.NewNamedWatcher(watchMgr, log)
	if err != nil {
		return err
	}
	r.watches = map[types.NamespacedName]controllers.Watch{}
	return ctrl.NewControllerManagedBy(mgr).
		For(&changegroupv1beta1.IPMapping{}).
		Complete(r)
}
