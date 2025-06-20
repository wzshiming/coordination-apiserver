/*
Copyright 2025 The Kubernetes Authors.

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

package registry

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	durationutil "k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/watch"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage/names"
)

var (
	errUIDMismatch             = fmt.Errorf("UID mismatch")
	errResourceVersionMismatch = fmt.Errorf("ResourceVersion mismatch")
)

type objcetKey struct {
	Name      string
	Namespace string
}

type leaseStorage struct {
	storeMut sync.RWMutex
	store    map[objcetKey]*coordinationv1.Lease

	watchesMut sync.RWMutex
	watches    map[*leaseStorageWatch]struct{}

	resourceVersion uint64
}

func NewMemoryStore() rest.Storage {
	return &leaseStorage{
		store:   map[objcetKey]*coordinationv1.Lease{},
		watches: map[*leaseStorageWatch]struct{}{},
	}
}

var _ interface {
	rest.Scoper
	rest.KindProvider
	rest.SingularNameProvider
	rest.StandardStorage
} = (*leaseStorage)(nil)

func (*leaseStorage) GetSingularName() string {
	return "lease"
}

func (*leaseStorage) Kind() string {
	return "Lease"
}

func (*leaseStorage) NamespaceScoped() bool {
	return true
}

func (*leaseStorage) New() runtime.Object {
	return &coordinationv1.Lease{}
}

func (*leaseStorage) Destroy() {}

func (l *leaseStorage) nextResourceVersion() string {
	return strconv.FormatUint(atomic.AddUint64(&l.resourceVersion, 1), 10)
}

func (l *leaseStorage) currentResourceVersion() string {
	return strconv.FormatUint(atomic.LoadUint64(&l.resourceVersion), 10)
}

func (l *leaseStorage) Create(ctx context.Context, obj runtime.Object, createValidation rest.ValidateObjectFunc, options *metav1.CreateOptions) (runtime.Object, error) {
	objectMeta, err := meta.Accessor(obj)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}

	if objectMeta.GetCreationTimestamp().UTC().IsZero() {
		objectMeta.SetCreationTimestamp(metav1.Now())
	}

	if objectMeta.GetUID() == "" {
		objectMeta.SetUID(uuid.NewUUID())
	}

	name := objectMeta.GetName()
	namespace := genericapirequest.NamespaceValue(ctx)
	if name == "" {
		generateName := objectMeta.GetGenerateName()
		if generateName != "" {
			name = names.SimpleNameGenerator.GenerateName(generateName)
			objectMeta.SetName(name)
		}
	}

	_, ok := l.get(namespace, name)
	if ok {
		return nil, errors.NewAlreadyExists(coordinationv1.Resource("leases"), name)
	}

	if createValidation != nil {
		if err := createValidation(ctx, obj); err != nil {
			return nil, err
		}
	}

	lease := obj.(*coordinationv1.Lease)
	lease.ResourceVersion = l.nextResourceVersion()
	l.put(namespace, name, lease)
	l.notifyWatchers(watch.Added, lease)

	return obj, nil
}

func (l *leaseStorage) put(namespace, name string, lease *coordinationv1.Lease) {
	key := objcetKey{Name: name, Namespace: namespace}

	l.storeMut.Lock()
	defer l.storeMut.Unlock()
	l.store[key] = lease
}

func (l *leaseStorage) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo, createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc, forceAllowCreate bool, options *metav1.UpdateOptions) (runtime.Object, bool, error) {
	namespace := genericapirequest.NamespaceValue(ctx)
	obj, ok := l.get(namespace, name)
	if !ok {
		if !forceAllowCreate {
			return nil, false, errors.NewNotFound(coordinationv1.Resource("leases"), name)
		}

		updated, err := objInfo.UpdatedObject(ctx, obj)
		if err != nil {
			return nil, false, err
		}

		if createValidation != nil {
			if err := createValidation(ctx, updated); err != nil {
				return nil, false, err
			}
		}

		lease := updated.(*coordinationv1.Lease)
		lease.ResourceVersion = l.nextResourceVersion()
		l.put(namespace, name, lease)
		l.notifyWatchers(watch.Added, lease)

		return updated, true, nil
	}

	updated, err := objInfo.UpdatedObject(ctx, obj)
	if err != nil {
		return nil, false, err
	}

	lease := updated.(*coordinationv1.Lease)
	if newUID, oldUID := lease.GetUID(), obj.GetUID(); newUID != "" && oldUID != "" && newUID != oldUID {
		return nil, false, errors.NewConflict(coordinationv1.Resource("leases"), name, errUIDMismatch)
	}

	if lease.ResourceVersion != "" && lease.ResourceVersion != obj.GetResourceVersion() {
		return nil, false, errors.NewConflict(coordinationv1.Resource("leases"), name, errResourceVersionMismatch)
	}

	if updateValidation != nil {
		if err = updateValidation(ctx, updated, obj); err != nil {
			return nil, false, err
		}
	}

	lease.ResourceVersion = l.nextResourceVersion()
	l.put(namespace, name, lease)
	l.notifyWatchers(watch.Modified, lease)

	return updated, false, nil
}

func (l *leaseStorage) Delete(ctx context.Context, name string, deleteValidation rest.ValidateObjectFunc, options *metav1.DeleteOptions) (runtime.Object, bool, error) {
	namespace := genericapirequest.NamespaceValue(ctx)
	obj, ok := l.get(namespace, name)
	if !ok {
		return nil, false, errors.NewNotFound(coordinationv1.Resource("leases"), name)
	}

	if deleteValidation != nil {
		if err := deleteValidation(ctx, obj); err != nil {
			return nil, false, err
		}
	}

	obj.ResourceVersion = l.nextResourceVersion()
	l.del(namespace, name)
	l.notifyWatchers(watch.Deleted, obj)

	return obj, true, nil
}

func (l *leaseStorage) del(namespace, name string) {
	key := objcetKey{Name: name, Namespace: namespace}

	l.storeMut.Lock()
	defer l.storeMut.Unlock()
	delete(l.store, key)
}

func (l *leaseStorage) DeleteCollection(ctx context.Context, deleteValidation rest.ValidateObjectFunc, options *metav1.DeleteOptions, listOptions *metainternalversion.ListOptions) (runtime.Object, error) {
	namespace := genericapirequest.NamespaceValue(ctx)
	items := l.list(namespace, listOptions)
	list := &coordinationv1.LeaseList{
		Items: items,
	}

	if deleteValidation != nil {
		if err := deleteValidation(ctx, list); err != nil {
			return nil, err
		}
	}

	list.ResourceVersion = l.nextResourceVersion()
	for _, key := range items {
		key.ResourceVersion = list.ResourceVersion
		l.del(key.Name, key.Namespace)
		l.notifyWatchers(watch.Deleted, &key)
	}

	return list, nil
}

func (l *leaseStorage) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	namespace := genericapirequest.NamespaceValue(ctx)
	obj, ok := l.get(namespace, name)
	if !ok {
		return nil, errors.NewNotFound(coordinationv1.Resource("leases"), name)
	}
	return obj, nil
}

func (l *leaseStorage) get(namespace, name string) (*coordinationv1.Lease, bool) {
	key := objcetKey{Name: name, Namespace: namespace}

	l.storeMut.RLock()
	defer l.storeMut.RUnlock()
	obj, ok := l.store[key]
	return obj, ok
}

func (l *leaseStorage) notifyWatchers(eventType watch.EventType, obj *coordinationv1.Lease) {
	l.watchesMut.RLock()
	watchers := make([]*leaseStorageWatch, 0, len(l.watches))
	for w := range l.watches {
		if l.filter(obj, w.namespace, w.options) {
			watchers = append(watchers, w)
		}
	}
	l.watchesMut.RUnlock()

	for i := 0; i != 10 && len(watchers) > 0; i++ {
		var pending []*leaseStorageWatch
		for _, w := range watchers {
			if w.IsStopped() {
				continue
			}
			select {
			case w.ch <- watch.Event{Type: eventType, Object: obj}:
			default:
				pending = append(pending, w)
			}
		}
		watchers = pending
	}
}

func (l *leaseStorage) Watch(ctx context.Context, options *metainternalversion.ListOptions) (watch.Interface, error) {
	namespace := genericapirequest.NamespaceValue(ctx)

	w := &leaseStorageWatch{
		f:         l,
		ch:        make(chan watch.Event, 64),
		namespace: namespace,
		options:   options,
	}

	l.watchesMut.Lock()
	l.watches[w] = struct{}{}
	l.watchesMut.Unlock()

	items := l.list(namespace, options)

	if len(items) != 0 {
		go func() {
			for _, item := range items {
				w.ch <- watch.Event{Type: watch.Added, Object: &item}
			}
		}()
	}
	return w, nil
}

func (*leaseStorage) NewList() runtime.Object {
	return &coordinationv1.LeaseList{}
}

func (l *leaseStorage) List(ctx context.Context, options *metainternalversion.ListOptions) (runtime.Object, error) {
	namespace := genericapirequest.NamespaceValue(ctx)
	items := l.list(namespace, options)

	softLeases(items)
	list := &coordinationv1.LeaseList{
		Items: items,
	}
	list.ResourceVersion = l.currentResourceVersion()
	return list, nil
}

func softLeases(items []coordinationv1.Lease) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Namespace != items[j].Namespace {
			return items[i].Namespace < items[j].Namespace
		}
		return items[i].Name < items[j].Name
	})
}

func (l *leaseStorage) list(namespace string, options *metainternalversion.ListOptions) []coordinationv1.Lease {
	l.storeMut.RLock()
	defer l.storeMut.RUnlock()
	items := make([]coordinationv1.Lease, 0, len(l.store))
	for _, obj := range l.store {
		if l.filter(obj, namespace, options) {
			items = append(items, *obj)
		}
	}
	return items
}

func (l *leaseStorage) filter(obj *coordinationv1.Lease, namespace string, options *metainternalversion.ListOptions) bool {
	if namespace != "" {
		if obj.Namespace != namespace {
			return false
		}
	}

	if options != nil {
		if labelSelector := options.LabelSelector; labelSelector != nil {
			if !labelSelector.Matches(labels.Set(obj.Labels)) {
				return false
			}
		}
		if fieldSelector := options.FieldSelector; fieldSelector != nil {
			if !fieldSelector.Matches(fields.Set{
				"metadata.name":      obj.Name,
				"metadata.namespace": obj.Namespace,
			}) {
				return false
			}
		}
		if rv := options.ResourceVersion; rv != "" && rv != "0" {
			objRV, err := strconv.ParseUint(obj.ResourceVersion, 10, 64)
			if err != nil {
				return false
			}
			reqRV, err := strconv.ParseUint(rv, 10, 64)
			if err != nil {
				return false
			}
			if objRV <= reqRV {
				return false
			}
		}
	}

	return true
}

func (*leaseStorage) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	var table metav1.Table

	table.ColumnDefinitions = []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: "Name is the name of the lease"},
		{Name: "Holder", Type: "string", Description: "HolderIdentity contains the identity of the holder of a current lease"},
		{Name: "Age", Type: "string", Description: "Age is the time since creation of the lease"},
	}

	switch t := object.(type) {
	case *coordinationv1.Lease:
		table.ResourceVersion = t.ResourceVersion
		addLeasesToTable(&table, *t)
	case *coordinationv1.LeaseList:
		table.ResourceVersion = t.ResourceVersion
		table.Continue = t.Continue
		addLeasesToTable(&table, t.Items...)
	default:
	}

	return &table, nil
}

func addLeasesToTable(table *metav1.Table, leases ...coordinationv1.Lease) {
	for _, lease := range leases {
		ts := "<unknown>"
		if timestamp := lease.CreationTimestamp; !timestamp.IsZero() {
			ts = durationutil.HumanDuration(time.Since(timestamp.Time))
		}
		table.Rows = append(table.Rows, metav1.TableRow{
			Cells:  []interface{}{lease.Name, *lease.Spec.HolderIdentity, ts},
			Object: runtime.RawExtension{Object: &lease},
		})
	}
}
