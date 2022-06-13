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

package controllers_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"change.me.later/ipmapping/controllers"
	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestFieldPath(t *testing.T) {
	_S := func(str string) *string {
		return &str
	}
	tests := []struct {
		obj   string
		path  string
		value *string
	}{
		{
			obj:   `{"status": {"ipAddress": "127.0.0.1"}}`,
			path:  ".status.ipAddress",
			value: _S("127.0.0.1"),
		},
		{
			obj:   `{"status": {"ipAddress": "127.0.0.1"}}`,
			path:  "status.ipAddress",
			value: _S("127.0.0.1"),
		},
		{
			obj:   `{"status": {"ipAddress": "127.0.0.1"}}`,
			path:  ".status.IP",
			value: nil,
		},
		{
			obj:   `{"status": {"ipAddress": "127.0.0.1"}}`,
			path:  ".spec.IP",
			value: nil,
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("Test %v", i), func(t *testing.T) {
			path := controllers.ParsePath(test.path)
			obj := unstructured.Unstructured{}
			if err := json.Unmarshal([]byte(test.obj), &obj.Object); err != nil {
				t.Fatalf("Fail to unmarshal object: %v", err)
			}
			got, err := path.Find(&obj)
			if err != nil {
				t.Fatalf("Fail to find value: %v", err)
			}
			if diff := cmp.Diff(got, test.value); diff != "" {
				t.Fatalf("Unexpected difference: %v", diff)
			}
		})
	}
}
