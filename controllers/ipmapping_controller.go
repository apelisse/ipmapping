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
	v1 "k8s.io/api/core/v1"
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

// IPMappingReconciler reconciles a IPMapping object
type IPMappingReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	watcher controllers.NamedWatcher
	watches map[types.NamespacedName]controllers.Watch
}

//+kubebuilder:rbac:groups=change.group.change.me.later,resources=ipmappings,verbs=get;list;watch
//+kubebuilder:rbac:groups=change.group.change.me.later,resources=ipmappings/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;update;patch;create;delete
//+kubebuilder:rbac:groups=core,resources=endpoints,verbs=get;list;update;patch;create;delete

// TODO: Remove that line
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch

func (r *IPMappingReconciler) ApplyService(ctx context.Context, ipMapping *changegroupv1beta1.IPMapping) error {
	service := v1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ipMapping.ObjectMeta.Name,
			Namespace: ipMapping.ObjectMeta.Namespace,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Port: 443, // TODO: Shouldn't be hard-coded
				},
			},
		},
	}
	return r.Patch(ctx, &service, client.Apply, client.FieldOwner("ipmapping_controller"), client.ForceOwnership)
}

func (r *IPMappingReconciler) ApplyEndpoints(ctx context.Context, ipMapping *changegroupv1beta1.IPMapping) error {
	if ipMapping.Status.IPAddress == nil {
		return r.DeleteEndpoints(ctx, ipMapping)
	}
	service := v1.Endpoints{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Endpoints",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ipMapping.ObjectMeta.Name,
			Namespace: ipMapping.ObjectMeta.Namespace,
		},
		Subsets: []v1.EndpointSubset{
			{
				Addresses: []v1.EndpointAddress{
					{
						IP: *ipMapping.Status.IPAddress,
					},
				},
				Ports: []v1.EndpointPort{
					{
						Port: 443, // TODO: Shouldn't be hard-coded
					},
				},
			},
		},
	}
	return r.Patch(ctx, &service, client.Apply, client.FieldOwner("ipmapping_controller"), client.ForceOwnership)
}

// Apply the given ipAddress to the status of the IPMapping. If the
// ipAddress pointer is nil, this will force to remove the ipMapping
// from the status, forcing the removal of the endpoint object.
func (r *IPMappingReconciler) ApplyIPMappingStatus(ctx context.Context, nn types.NamespacedName, ipAddress *string) error {
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
	return r.Status().Patch(ctx, &mapping, client.Apply, client.FieldOwner("ipmapping_controller"), client.ForceOwnership)
}

func (r IPMappingReconciler) DeleteService(ctx context.Context, ipMapping *changegroupv1beta1.IPMapping) error {
	service := v1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ipMapping.ObjectMeta.Name,
			Namespace: ipMapping.ObjectMeta.Namespace,
		},
	}
	return r.Delete(ctx, &service)
}

func (r IPMappingReconciler) DeleteEndpoints(ctx context.Context, ipMapping *changegroupv1beta1.IPMapping) error {
	endpoint := v1.Endpoints{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Endpoints",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ipMapping.ObjectMeta.Name,
			Namespace: ipMapping.ObjectMeta.Namespace,
		},
	}
	return r.Delete(ctx, &endpoint)
}

func findField(obj runtime.Object, path *string) (*string, error) {
	if path == nil {
		return nil, nil
	}
	p := ParsePath(*path)
	return p.Find(obj.(*unstructured.Unstructured))
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *IPMappingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.Info("Reconciling")

	// TODO: I'm not sure this should be here (or way further, below).
	// Update the watch and configure it to update the mapping status.
	if watch, ok := r.watches[req.NamespacedName]; ok {
		log.Info("Deleting watch")
		watch.Stop()
		delete(r.watches, req.NamespacedName)
	}

	var ipMapping changegroupv1beta1.IPMapping
	if err := r.Get(ctx, req.NamespacedName, &ipMapping); err != nil {
		if apierrors.IsNotFound(err) {
			if r.DeleteService(ctx, &ipMapping); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to delete service: %v", err)
			}
			if r.DeleteEndpoints(ctx, &ipMapping); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to delete service: %v", err)
			}
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if err := r.ApplyService(ctx, &ipMapping); err != nil {
		return ctrl.Result{}, fmt.Errorf("Failed to apply service: %v", err)
	}
	if err := r.ApplyEndpoints(ctx, &ipMapping); err != nil {
		return ctrl.Result{}, fmt.Errorf("Failed to apply endpoints: %v", err)
	}

	// TODO: This shouldn't use the unsafe form.
	gvr, _ := meta.UnsafeGuessKindToResource(schema.FromAPIVersionAndKind(ipMapping.Spec.TargetRef.APIVersion, ipMapping.Spec.TargetRef.Kind))
	watch, err := r.watcher.Watch(gvr, req.Namespace, ipMapping.Spec.TargetRef.Name, func(lister cache.GenericLister) error {
		log := log.WithName("WatchHandler")
		log.Info("Event received", "gvr", gvr, "namespace", req.Namespace, "name", req.Name)
		obj, err := lister.Get(req.Name)
		var ipAddress *string
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		} else if err != nil {
			ipAddress, err = findField(obj, ipMapping.Spec.TargetRef.FieldPath)
			if err != nil {
				return err
			}
		} else {
			log.Info("Target deleted", "gvr", gvr, "namespace", req.Namespace, "name", req.Name)
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
func (r *IPMappingReconciler) SetupWithManager(mgr ctrl.Manager, log logr.Logger) error {
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
