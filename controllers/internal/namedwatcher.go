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
	"sync"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	cache "k8s.io/client-go/tools/cache"
)

// NamedWatcher watches a specific gvrnn and calls the handler with no
// parameters (since the key should already be known by the handler).
//
// This internally maintains a list of watches for each GVRN and
// dispatch by name accordingly. When a watch is stopped, it doesn't
// necessarily stops the underlying watch if another name in the same
// gvrn is still being watched. The watch is removed once the GVRN
// doesn't need to be watched anymore.
type NamedWatcher interface {
	// Watch creates and starts a new watch for the given GVRNN. If
	// the watch can't be started, an error is returned. The watch
	// can be stopped by calling its Stop method.
	//
	// The NamedWatcher can't be used from within the handler
	// (neither to start a new watch nor to stop a watch) in order
	// to avoid deadlocks.
	Watch(gvr schema.GroupVersionResource, namespace string, name string, handler func(lister cache.GenericLister) error) (Watch, error)
}

// NewNamedWatcher creates
func NewNamedWatcher(watchManager WatchManager, log logr.Logger) NamedWatcher {
	return &namedWatcher{
		watchManager: watchManager,
		watches:      make(map[gvrn]*gvrnWatch),
		log:          log.WithName("NamedWatcher"),
	}
}

type gvrn struct {
	gvr       schema.GroupVersionResource
	namespace string
}

type namedWatcher struct {
	watchManager WatchManager
	watches      map[gvrn]*gvrnWatch
	log          logr.Logger
	mutex        sync.Mutex
}

func (n *namedWatcher) newGVRNWatch(gvr schema.GroupVersionResource, namespace string) (*gvrnWatch, error) {
	gvrnWatch := gvrnWatch{
		names: map[string][]*namedWatch{},
	}
	var err error
	gvrnWatch.watch, err = n.watchManager.Watch(gvr, namespace, gvrnWatch.handle)
	if err != nil {
		return nil, err
	}
	return &gvrnWatch, nil
}

func (n *namedWatcher) Watch(gvr schema.GroupVersionResource, namespace string, name string, handler func(cache.GenericLister) error) (Watch, error) {
	n.log.Info("Adding new watch", "gvr", gvr, "namespace", namespace, "name", name)
	n.mutex.Lock()
	defer n.mutex.Unlock()
	gvrn := gvrn{gvr: gvr, namespace: namespace}
	watch, ok := n.watches[gvrn]
	if !ok {
		var err error
		watch, err = n.newGVRNWatch(gvr, namespace)
		if err != nil {
			return nil, err
		}
		n.watches[gvrn] = watch
	}
	namedWatch := &namedWatch{
		watcher:   n,
		handler:   handler,
		gvr:       gvr,
		namespace: namespace,
		name:      name,
	}
	watch.add(name, namedWatch)
	return namedWatch, nil
}

func (n *namedWatcher) remove(watch *namedWatch) {
	n.log.Info("Removing watch", "gvr", watch.gvr, "namespace", watch.namespace, "name", watch.name)
	n.mutex.Lock()
	defer n.mutex.Unlock()
	gvrn := gvrn{gvr: watch.gvr, namespace: watch.namespace}
	gvrnWatch, ok := n.watches[gvrn]
	if !ok {
		panic(fmt.Errorf("Trying to remove unknown watch: %v", gvrn))
	}
	if gvrnWatch.remove(watch.name, watch) {
		delete(n.watches, gvrn)
	}
}

type gvrnWatch struct {
	watch Watch
	// For each name, we can have multiple watches.
	names map[string][]*namedWatch
}

// Remove the given name from the watch list.
// Returns true if this was the last item present and the watch
// has been cleared.
func (g *gvrnWatch) remove(name string, watch *namedWatch) bool {
	if watches, ok := g.names[name]; ok {
		for i := range watches {
			if watches[i] == watch {
				g.names[name] = append(watches[:i], watches[i+1:]...)
				break
			}
		}
		if len(g.names[name]) == 0 {
			delete(g.names, name)
		}
		if len(g.names) == 0 {
			g.watch.Stop()
			return true
		}
	}
	return false
}

func (g *gvrnWatch) handle(lister cache.GenericLister, name string) error {
	hasErr := false
	for _, watches := range g.names[name] {
		err := watches.handler(lister)
		if err != nil {
			utilruntime.HandleError(err)
			hasErr = true
		}
	}
	if hasErr {
		return fmt.Errorf("Error processing item: %v", name)
	}
	return nil
}

// Append a new watch for that name.
func (g *gvrnWatch) add(name string, watch *namedWatch) {
	g.names[name] = append(g.names[name], watch)
}

type namedWatch struct {
	watcher   *namedWatcher
	handler   func(cache.GenericLister) error
	gvr       schema.GroupVersionResource
	namespace string
	name      string
}

// Stop the given namedWatch.
func (n *namedWatch) Stop() {
	n.watcher.remove(n)
}
