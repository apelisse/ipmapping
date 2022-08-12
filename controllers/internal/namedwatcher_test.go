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
	"fmt"
	"testing"

	controllers "change.me.later/ipmapping/controllers/internal"
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	"k8s.io/apimachinery/pkg/runtime/schema"
	cache "k8s.io/client-go/tools/cache"
)

type FakeWatchManager struct {
	watches map[schema.GroupVersionResource]map[string]FakeWatch
	log     logr.Logger
}

func NewFakeWatchManager(log logr.Logger) *FakeWatchManager {
	return &FakeWatchManager{
		watches: make(map[schema.GroupVersionResource]map[string]FakeWatch),
		log:     log.WithName("FakeWatchManager"),
	}
}

func (f *FakeWatchManager) Watch(gvr schema.GroupVersionResource, namespace string, handler func(_ cache.GenericNamespaceLister, name string) error) (controllers.Watch, error) {
	f.log.Info("Watch", "gvr", gvr, "namespace", namespace)
	if gvrw, ok := f.watches[gvr]; ok {
		if _, ok := gvrw[namespace]; ok {
			panic(fmt.Errorf("Watch is already set-up for %v/%v", gvr, namespace))
		}
	} else {
		f.watches[gvr] = make(map[string]FakeWatch)
	}
	watch := FakeWatch{
		manager:   f,
		gvr:       gvr,
		namespace: namespace,
		handler:   handler,
		log:       f.log.WithName("FakeWatch"),
	}
	f.watches[gvr][namespace] = watch
	return &watch, nil
}

// Calls an event on the given name for the watch.
func (f *FakeWatchManager) Event(gvr schema.GroupVersionResource, namespace string, name string) {
	f.log.Info("Event", "gvr", gvr, "namespace", namespace, "name", name)
	gvrw, ok := f.watches[gvr]
	if !ok {
		panic(fmt.Errorf("Can't create event for missing watch on gvr: %v", gvr))
	}
	watch, ok := gvrw[namespace]
	if !ok {
		panic(fmt.Errorf("Can't create event for missing watch on gvr/namespace: %v/%v", gvr, namespace))
	}
	if err := watch.handler(nil, name); err != nil {
		panic(fmt.Errorf("Failed to run watch handler: %v", err))
	}
}

// Returns true if there is no watch currently set. That's useful to
// check at the end of a test if a Watch is still active.
func (f *FakeWatchManager) Empty() bool {
	return len(f.watches) == 0
}

type FakeWatch struct {
	manager   *FakeWatchManager
	gvr       schema.GroupVersionResource
	namespace string
	handler   func(lister cache.GenericNamespaceLister, name string) error
	log       logr.Logger
}

func (f *FakeWatch) Stop() {
	f.log.Info("Stop", "gvr", f.gvr, "namespace", f.namespace)
	if gvrw, ok := f.manager.watches[f.gvr]; ok {
		if _, ok := gvrw[f.namespace]; !ok {
			panic(fmt.Errorf("Watch doesn't exist for %v/%v", f.gvr, f.namespace))
		}
		delete(gvrw, f.namespace)
	} else {
		panic(fmt.Errorf("Watch doesn't exist for %v", f.gvr))
	}
	if len(f.manager.watches[f.gvr]) == 0 {
		delete(f.manager.watches, f.gvr)
	}
}

type Counter map[string]int

func (c Counter) Handler(name string) func(cache.GenericNamespaceLister) error {
	return func(_ cache.GenericNamespaceLister) error {
		c[name] += 1
		return nil
	}
}

func (c Counter) Check(t *testing.T, name string, wanted int) {
	t.Helper()
	if got := c[name]; got != wanted {
		t.Fatalf("Expected count for %q = %v, got %v", name, wanted, got)
	}
}

func TestNamedWatcher_CreateWatchDelete(t *testing.T) {
	fakeManager := NewFakeWatchManager(testr.New(t))
	watcher := controllers.NewNamedWatcher(fakeManager, testr.New(t))
	counts := Counter{}

	pod1, err := watcher.Watch(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod1", counts.Handler("pod1"))
	if err != nil {
		t.Fatalf("Failed to start watch: %v", err)
	}
	fakeManager.Event(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod1")
	fakeManager.Event(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod2")
	counts.Check(t, "pod1", 1)
	counts.Check(t, "pod2", 0)
	pod1.Stop()
	if !fakeManager.Empty() {
		t.Fatalf("Some watches are left running: %v", fakeManager)
	}
}

func TestNamedWatcher_MultipleWithSameNamespace(t *testing.T) {
	fakeManager := NewFakeWatchManager(testr.New(t))
	watcher := controllers.NewNamedWatcher(fakeManager, testr.New(t))
	counts := Counter{}

	pod1, err := watcher.Watch(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod1", counts.Handler("pod1"))
	if err != nil {
		t.Fatalf("Failed to start watch: %v", err)
	}
	fakeManager.Event(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod1")
	fakeManager.Event(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod2")
	counts.Check(t, "pod1", 1)
	counts.Check(t, "pod2", 0)
	// Add new watch for the same combination
	pod2, err := watcher.Watch(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod2", counts.Handler("pod2"))
	if err != nil {
		t.Fatalf("Failed to start watch: %v", err)
	}
	fakeManager.Event(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod1")
	fakeManager.Event(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod2")
	counts.Check(t, "pod1", 2)
	counts.Check(t, "pod2", 1)

	pod1.Stop()
	pod2.Stop()

	if !fakeManager.Empty() {
		t.Fatalf("Some watches are left running: %v", fakeManager)
	}
}

func TestNamedWatcher_MultipleWithSameEverything(t *testing.T) {
	fakeManager := NewFakeWatchManager(testr.New(t))
	watcher := controllers.NewNamedWatcher(fakeManager, testr.New(t))
	counts := Counter{}

	pod1, err := watcher.Watch(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod1", counts.Handler("pod1"))
	if err != nil {
		t.Fatalf("Failed to start watch: %v", err)
	}
	fakeManager.Event(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod1")
	counts.Check(t, "pod1", 1)
	counts.Check(t, "pod1bis", 0)
	// Add new watch for the same combination
	pod1bis, err := watcher.Watch(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod1", counts.Handler("pod1bis"))
	if err != nil {
		t.Fatalf("Failed to start watch: %v", err)
	}
	fakeManager.Event(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod1")
	counts.Check(t, "pod1", 2)
	counts.Check(t, "pod1bis", 1)

	pod1.Stop()
	pod1bis.Stop()

	if !fakeManager.Empty() {
		t.Fatalf("Some watches are left running: %v", fakeManager)
	}
}

func TestNamedWatcher_DifferentNamespace(t *testing.T) {
	fakeManager := NewFakeWatchManager(testr.New(t))
	watcher := controllers.NewNamedWatcher(fakeManager, testr.New(t))
	counts := Counter{}

	pod1, err := watcher.Watch(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod1", counts.Handler("pod1"))
	if err != nil {
		t.Fatalf("Failed to start watch: %v", err)
	}
	fakeManager.Event(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod1")
	counts.Check(t, "pod1", 1)
	counts.Check(t, "namespace/pod2", 0)
	// Add new watch for the same combination
	pod2, err := watcher.Watch(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "namespace", "pod2", counts.Handler("namespace/pod2"))
	if err != nil {
		t.Fatalf("Failed to start watch: %v", err)
	}
	fakeManager.Event(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod1")
	fakeManager.Event(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "namespace", "pod2")
	counts.Check(t, "pod1", 2)
	counts.Check(t, "namespace/pod2", 1)

	pod1.Stop()
	pod2.Stop()
	if !fakeManager.Empty() {
		t.Fatalf("Some watches are left running: %v", fakeManager)
	}
}

func TestNamedWatcher_DifferentTypes(t *testing.T) {
	fakeManager := NewFakeWatchManager(testr.New(t))
	watcher := controllers.NewNamedWatcher(fakeManager, testr.New(t))
	counts := Counter{}

	pod1, err := watcher.Watch(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod1", counts.Handler("pod1"))
	if err != nil {
		t.Fatalf("Failed to start watch: %v", err)
	}
	fakeManager.Event(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod1")
	fakeManager.Event(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod2")
	counts.Check(t, "pod1", 1)
	counts.Check(t, "pod2", 0)

	service1, err := watcher.Watch(schema.GroupVersionResource{Version: "v1", Resource: "services"}, "default", "service1", counts.Handler("service1"))
	if err != nil {
		t.Fatalf("Failed to start watch: %v", err)
	}
	fakeManager.Event(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "default", "pod1")
	fakeManager.Event(schema.GroupVersionResource{Version: "v1", Resource: "services"}, "default", "service1")
	fakeManager.Event(schema.GroupVersionResource{Version: "v1", Resource: "services"}, "default", "service2")
	counts.Check(t, "pod1", 2)
	counts.Check(t, "pod2", 0)
	counts.Check(t, "service1", 1)
	counts.Check(t, "service2", 0)

	pod1.Stop()
	service1.Stop()
	if !fakeManager.Empty() {
		t.Fatalf("Some watches are left running: %v", fakeManager)
	}
}
