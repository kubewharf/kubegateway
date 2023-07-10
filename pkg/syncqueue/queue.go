// Copyright 2022 ByteDance and its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package syncqueue

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

// Result contains the result of a Reconciler invocation.
type Result struct {
	// Requeue tells the Queue to requeue the object. Defaults to false.
	Requeue bool

	// RequeueAfter if greater than 0, tells the Queue to requeue the object after the Duration.
	// Implies that Requeue is true, there is no need to set Requeue to true at the same time as RequeueAfter.
	RequeueAfter time.Duration

	// MaxRequeueTimes tells the Queue the limit count of requeueing object. Defaults to 0.
	// It only take effect when Requeue is true or RequeueAfter is greater than 0.
	MaxRequeueTimes int
}

const (
	maxErrRetries = 3
)

var (
	// defaultKeyFunc is the default key function
	defaultKeyFunc = cache.DeletionHandlingMetaNamespaceKeyFunc
)

// SyncHandler describes the function which will be
// called for each item in the queue
//
// if the sync handler returns an *ErrRetryAfter error, the queue
// will put obj back and retry after a period of time
type SyncHandler func(obj interface{}) (Result, error)

// KeyFunc describes a function that generates a key from a object
type KeyFunc func(obj interface{}) (interface{}, error)

// PassthroughKeyFunc is a keyFunc which returns the original obj
func PassthroughKeyFunc(obj interface{}) (interface{}, error) {
	return obj, nil
}

// SyncQueue is a helper for creating a kubernetes controller easily
// It requires a syncHandler and an optional key function.
// After running the syncQueue, you can call it's Enqueque function to enqueue items.
// SyncQueue will get key from the items by keyFunc, and add the key to the rate limit workqueue.
// The worker will be invoked to call the syncHandler.
type SyncQueue struct {
	// gvk is the GroupVersionKind of synced object
	gvk schema.GroupVersionKind
	// queue is the work queue the worker polls
	queue workqueue.RateLimitingInterface
	// syncHandler is called for each item in the queue
	syncHandler SyncHandler
	// KeyFunc is called to get key from obj
	keyFunc KeyFunc

	waitGroup sync.WaitGroup

	maxErrRetries int
	stopCh        chan struct{}
}

// NewSyncQueue returns a new SyncQueue, enqueue key of obj using default keyFunc
func NewSyncQueue(gvk schema.GroupVersionKind, syncHandler SyncHandler) *SyncQueue {
	return NewCustomSyncQueue(gvk, syncHandler, nil)
}

// NewPassthroughSyncQueue returns a new SyncQueue with PassthroughKeyFunc
func NewPassthroughSyncQueue(gvk schema.GroupVersionKind, syncHandler SyncHandler) *SyncQueue {
	return NewCustomSyncQueue(gvk, syncHandler, PassthroughKeyFunc)
}

// NewCustomSyncQueue returns a new SyncQueue using custom keyFunc
func NewCustomSyncQueue(gvk schema.GroupVersionKind, syncHandler SyncHandler, keyFunc KeyFunc) *SyncQueue {
	sq := &SyncQueue{
		gvk:           gvk,
		queue:         workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		syncHandler:   syncHandler,
		keyFunc:       keyFunc,
		waitGroup:     sync.WaitGroup{},
		maxErrRetries: maxErrRetries,
		stopCh:        make(chan struct{}),
	}

	if keyFunc == nil {
		sq.keyFunc = sq.defaultKeyFunc
	}

	return sq
}

// Run starts n workers to sync
func (sq *SyncQueue) Run(workers int) {
	for i := 0; i < workers; i++ {
		go wait.Until(sq.worker, time.Second, sq.stopCh)
	}
}

// ShutDown shuts down the work queue and waits for the worker to ACK
func (sq *SyncQueue) ShutDown() {
	close(sq.stopCh)
	// sq shutdown the queue, then worker can't get key from queue
	// processNextWorkItem return false, and then waitGroup -1
	sq.queue.ShutDown()
	sq.waitGroup.Wait()
}

// IsShuttingDown returns if the method Shutdown was invoked
func (sq *SyncQueue) IsShuttingDown() bool {
	return sq.queue.ShuttingDown()
}

// SetMaxRetries sets the max retry times of the queue
func (sq *SyncQueue) SetMaxErrRetries(max int) {
	if max > 0 {
		sq.maxErrRetries = max
	}
}

// Queue returns the rate limit work queue
func (sq *SyncQueue) Queue() workqueue.RateLimitingInterface {
	return sq.queue
}

// Enqueue wraps queue.Add
func (sq *SyncQueue) Enqueue(obj interface{}) {
	if sq.IsShuttingDown() {
		return
	}
	key, err := sq.keyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("failed to get key for %v %#v: %v", sq.gvk, obj, err))
		return
	}
	sq.queue.Add(key)
}

// EnqueueRateLimited wraps queue.AddRateLimited. It adds an item to the workqueue
// after the rate limiter says its ok
func (sq *SyncQueue) EnqueueRateLimited(obj interface{}) {
	if sq.IsShuttingDown() {
		return
	}
	key, err := sq.keyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("failed to get key for %v %#v: %v", sq.gvk, obj, err))
		return
	}
	sq.queue.AddRateLimited(key)
}

// EnqueueAfter wraps queue.AddAfter. It adds an item to the workqueue after the indicated duration has passed
func (sq *SyncQueue) EnqueueAfter(obj interface{}, after time.Duration) {
	if sq.IsShuttingDown() {
		return
	}
	key, err := sq.keyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("failed to get key for %v %#v: %v", sq.gvk, obj, err))
		return
	}
	sq.queue.AddAfter(key, after)
}

// Dequeue calls the RateLimitingInterface.Froget and Done to stop tracking the item
func (sq *SyncQueue) Dequeue(obj interface{}) {
	if sq.IsShuttingDown() {
		return
	}
	key, err := sq.keyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("failed to get key for %v %#v: %v", sq.gvk, obj, err))
		return
	}
	sq.queue.Forget(key)
	sq.queue.Done(key)
}

func (sq *SyncQueue) defaultKeyFunc(obj interface{}) (interface{}, error) {
	key, err := defaultKeyFunc(obj)
	if err != nil {
		return "", err
	}
	return key, nil
}

// Worker is a common worker for controllers
// worker runs a worker thread that just dequeues items, processes them, and marks them done.
// It enforces that the syncHandler is never invoked concurrently with the same key.
func (sq *SyncQueue) worker() {
	sq.waitGroup.Add(1)
	defer sq.waitGroup.Done()
	// invoked oncely process any until exhausted
	for sq.processNextWorkItem() {
	}
}

// ProcessNextWorkItem processes next item in queue by syncHandler
func (sq *SyncQueue) processNextWorkItem() bool {
	obj, quit := sq.queue.Get()
	if quit {
		return false
	}
	defer sq.queue.Done(obj)

	result, err := sq.syncHandler(obj)
	if err != nil {
		var key interface{}
		// get short key no matter what the keyfunc is
		keyS, kerr := defaultKeyFunc(obj)
		if kerr != nil {
			key = obj
		} else {
			key = keyS
		}
		if sq.queue.NumRequeues(obj) < sq.maxErrRetries {
			klog.Warningf("error syncing object (gvk: %v, key: %v) retry: %v, err: %v", sq.gvk, key, sq.queue.NumRequeues(obj), err)
			sq.queue.AddRateLimited(obj)
			return false
		}
		klog.Warningf("dropping object (gvk: %v, key: %v) from the queue", sq.gvk, key)
		sq.queue.Forget(obj)
		return true
	}

	var requeueAfter time.Duration
	if result.RequeueAfter > 0 {
		requeueAfter = result.RequeueAfter
	} else if result.Requeue {
		requeueAfter = time.Millisecond
	}
	if requeueAfter > 0 && result.MaxRequeueTimes > 0 &&
		sq.queue.NumRequeues(obj) >= result.MaxRequeueTimes {
		// more than maximum requeue times
		// skip requeue
		requeueAfter = 0
	}

	// we should forget this obj in 2 case
	// 1. MaxRequeueTimes is not set
	// 2. requeueAfter == 0 means there is no need to requeue this obj
	if requeueAfter == 0 || result.MaxRequeueTimes == 0 {
		defer sq.queue.Forget(obj)
	}

	if requeueAfter > 0 {
		sq.EnqueueAfter(obj, requeueAfter)
		return true
	}

	// no err and no requeue
	return true
}

func (sq *SyncQueue) ResourceEventHandler(scheme *runtime.Scheme) cache.ResourceEventHandler {
	enqueue := func(action string, obj interface{}) {
		runtimeObj, ok := obj.(runtime.Object)
		if !ok {
			return
		}

		err := isExpectedGVK(scheme, sq.gvk, runtimeObj)
		if err != nil {
			tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
			if !ok {
				// this obj is not the expected gvk or tombstone
				utilruntime.HandleError(err)
				return
			}
			runtimeObj, ok = tombstone.Obj.(runtime.Object)
			if !ok {
				utilruntime.HandleError(err)
				return
			}
			err := isExpectedGVK(scheme, sq.gvk, runtimeObj)
			if err != nil {
				utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a %v %#v", sq.gvk.String(), obj))
				return
			}
		}

		var key interface{}
		// get short key no matter what the keyfunc is
		key, kerr := defaultKeyFunc(runtimeObj)
		if kerr != nil {
			key = runtimeObj
		}

		klog.V(5).Infof("%v obj gvk %v, key %v", action, sq.gvk, key)
		sq.Enqueue(runtimeObj)
	}
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			enqueue("Add", obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			enqueue("Update", newObj)
		},
		DeleteFunc: func(obj interface{}) {
			enqueue("Delete", obj)
		},
	}
}

func (sq *SyncQueue) FilteringResourceEventHandler(scheme *runtime.Scheme, filter func(obj interface{}) bool) cache.ResourceEventHandler {
	return cache.FilteringResourceEventHandler{
		FilterFunc: filter,
		Handler:    sq.ResourceEventHandler(scheme),
	}
}

func isExpectedGVK(scheme *runtime.Scheme, gvk schema.GroupVersionKind, obj runtime.Object) error {
	gvks, _, err := scheme.ObjectKinds(obj)
	if err != nil {
		return fmt.Errorf("failed to detect groupVersionKind from runtime.Object %T with scheme %T: %v", obj, scheme, err)
	}
	if len(gvks) == 0 {
		return fmt.Errorf("failed to detect groupVersionKind from runtime.Object %T with scheme %T", obj, scheme)
	}

	found := false
	for _, item := range gvks {
		if reflect.DeepEqual(item, gvk) {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("expected runtime obj's gvk %v, got %v", gvk, gvks)
	}
	return nil
}
