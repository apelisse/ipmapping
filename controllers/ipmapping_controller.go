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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

func (r *IPMappingReconciler) ApplyService(ctx context.Context, ipMapping *changegroupv1beta1.IPMapping) error {
	service := v1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ipMapping.ObjectMeta.Name,
			Namespace: ipMapping.ObjectMeta.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: ipMapping.APIVersion,
					Kind:       ipMapping.Kind,
					Name:       ipMapping.Name,
					UID:        ipMapping.UID,
				},
			},
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
	subsets := []v1.EndpointSubset{}
	if ipMapping.Status.IPAddress != nil {
		subsets = append(subsets, v1.EndpointSubset{
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
		})
	}
	service := v1.Endpoints{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Endpoints",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ipMapping.ObjectMeta.Name,
			Namespace: ipMapping.ObjectMeta.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: ipMapping.APIVersion,
					Kind:       ipMapping.Kind,
					Name:       ipMapping.Name,
					UID:        ipMapping.UID,
				},
			},
		},
		Subsets: subsets,
	}
	return r.Patch(ctx, &service, client.Apply, client.FieldOwner("ipmapping_controller"), client.ForceOwnership)
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *IPMappingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.Info("Reconciling")

	var ipMapping changegroupv1beta1.IPMapping

	if err := r.Get(ctx, req.NamespacedName, &ipMapping); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if err := r.ApplyService(ctx, &ipMapping); err != nil {
		return ctrl.Result{}, fmt.Errorf("Failed to apply service: %v", err)
	}
	if err := r.ApplyEndpoints(ctx, &ipMapping); err != nil {
		return ctrl.Result{}, fmt.Errorf("Failed to apply endpoints: %v", err)
	}

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
