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
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Path []string

func (p Path) Find(obj *unstructured.Unstructured) (*string, error) {
	s, found, err := unstructured.NestedString(obj.Object, p...)
	if !found {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &s, nil
}

func ParsePath(field string) Path {
	// Split the string by `.` pretty much ...
	split := strings.Split(field, ".")
	// Removing first item if empty (when path starts with .)
	if len(split) > 0 && split[0] == "" {
		split = split[1:]
	}
	return split
}
