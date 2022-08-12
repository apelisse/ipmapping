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

package v1beta1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IPMappingSpec defines the desired state of IPMapping
type IPMappingSpec struct {
	// targetRef is the object and its path that should be used to
	// find the IP address.
	TargetRef ObjectReference `json:"targetRef"`

	// Ports is the list of ports that the endpoints should listen
	// to.
	// +listType=map
	// +listMapKey=port
	// +listMapKey=protocol
	Ports []IPMappingPort `json:"ports"`
}

// ObjectReference identifies where to find the IP address that needs to
// be mapped with a service.
type ObjectReference struct {
	// APIVersion of the object to watch.
	APIVersion string `json:"apiVersion"`

	// Kind of the resource to watch.
	Kind string `json:"kind"`

	// Name of the resource to watch.
	Name string `json:"name"`

	// Path to the IP Field that needs to be mapped.
	// +optional
	FieldPath *string `json:"fieldPath,omitempty"`
}

// IPMappingPort describes the port that the endpoints should listen to.
type IPMappingPort struct {
	// Port number to listen to.
	Port int32 `json:"port"`
	// Protocol for this port.
	// +kubebuilder:validation:Enum=TCP;UDP;SCTP
	// +default=TCP
	Protocol v1.Protocol `json:"protocol,omitempty"`
}

// IPMappingStatus defines the observed state of IPMapping
type IPMappingStatus struct {
	// TODO: That certainly misses `Conditions`, but I'm too lazy
	// right now to do it, and we can live without it for now.

	// IPAddress is the IP that we've read from the target object
	// and that is used for the endpoint.
	// +optional
	IPAddress *string `json:"ipAddress"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// IPMapping is the Schema for the ipmappings API
type IPMapping struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IPMappingSpec   `json:"spec,omitempty"`
	Status IPMappingStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// IPMappingList contains a list of IPMapping
type IPMappingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IPMapping `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IPMapping{}, &IPMappingList{})
}
