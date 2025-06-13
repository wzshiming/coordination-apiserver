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
	"sync"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	durationutil "k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/apimachinery/pkg/watch"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage/names"
)

type objcetKey struct {
	Name      string
	Namespace string
}

type leaseStorage struct {
	sync.RWMutex
	store   map[objcetKey]*coordinationv1.Lease
	watches map[*watch.FakeWatcher]struct{}
}

func NewMemoryStore() rest.Storage {
	return &leaseStorage{
		store:   map[objcetKey]*coordinationv1.Lease{},
		watches: map[*watch.FakeWatcher]struct{}{},
	}
}

var _ interface {
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

func (l *leaseStorage) Create(ctx context.Context, obj runtime.Object, createValidation rest.ValidateObjectFunc, options *metav1.CreateOptions) (runtime.Object, error) {
	objectMeta, err := meta.Accessor(obj)
	if err != nil {
		return nil, errors.NewInternalError(err)
	}

	rest.FillObjectMetaSystemFields(objectMeta)
	name := objectMeta.GetName()
	namespace := objectMeta.GetNamespace()

	if name == "" {
		generateName := objectMeta.GetGenerateName()
		if generateName != "" {
			name = names.SimpleNameGenerator.GenerateName(generateName)
			objectMeta.SetName(name)
		}
	}

	key := objcetKey{Name: name, Namespace: namespace}
	l.RLock()
	_, ok := l.store[key]
	l.RUnlock()
	if ok {
		return nil, errors.NewAlreadyExists(coordinationv1.Resource("leases"), fmt.Sprintf("%s/%s", namespace, name))
	}

	if createValidation != nil {
		if err := createValidation(ctx, obj); err != nil {
			return nil, errors.NewBadRequest(err.Error())
		}
	}

	l.Lock()
	l.store[key] = obj.(*coordinationv1.Lease)
	l.Unlock()

	l.notifyWatchers(watch.Added, obj)
	return obj, nil
}

func (l *leaseStorage) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo, createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc, forceAllowCreate bool, options *metav1.UpdateOptions) (runtime.Object, bool, error) {
	namespace := genericapirequest.NamespaceValue(ctx)
	key := objcetKey{Name: name, Namespace: namespace}

	l.RLock()
	obj, ok := l.store[key]
	l.RUnlock()
	if !ok {
		obj := l.New()
		obj, err := objInfo.UpdatedObject(ctx, obj)
		if err != nil {
			return nil, false, errors.NewInternalError(err)
		}

		obj, err = l.Create(ctx, obj, createValidation, nil)
		if err != nil {
			return nil, false, err
		}
		return obj, true, nil
	}

	updated, err := objInfo.UpdatedObject(ctx, obj)
	if err != nil {
		return nil, false, errors.NewInternalError(err)
	}

	if updateValidation != nil {
		if err = updateValidation(ctx, updated, obj); err != nil {
			return nil, false, errors.NewBadRequest(err.Error())
		}
	}

	l.Lock()
	l.store[key] = updated.(*coordinationv1.Lease)
	l.Unlock()

	l.notifyWatchers(watch.Modified, updated)
	return updated, false, nil
}

func (l *leaseStorage) Delete(ctx context.Context, name string, deleteValidation rest.ValidateObjectFunc, options *metav1.DeleteOptions) (runtime.Object, bool, error) {
	namespace := genericapirequest.NamespaceValue(ctx)
	key := objcetKey{Name: name, Namespace: namespace}

	l.RLock()
	obj, ok := l.store[key]
	l.RUnlock()
	if !ok {
		return nil, false, errors.NewNotFound(coordinationv1.Resource("leases"), fmt.Sprintf("%s/%s", namespace, name))
	}

	if deleteValidation != nil {
		if err := deleteValidation(ctx, obj); err != nil {
			return nil, false, errors.NewBadRequest(err.Error())
		}
	}

	l.Lock()
	delete(l.store, key)
	l.Unlock()

	l.notifyWatchers(watch.Deleted, obj)
	return obj, true, nil
}

func (l *leaseStorage) DeleteCollection(ctx context.Context, deleteValidation rest.ValidateObjectFunc, options *metav1.DeleteOptions, listOptions *metainternalversion.ListOptions) (runtime.Object, error) {
	items := l.list(ctx, listOptions)

	if deleteValidation != nil {
		if err := deleteValidation(ctx, items); err != nil {
			return nil, errors.NewBadRequest(err.Error())
		}
	}

	l.Lock()
	defer l.Unlock()
	for _, key := range items.Items {
		key := objcetKey{Name: key.Name, Namespace: key.Namespace}
		delete(l.store, key)
	}
	return items, nil
}

func (l *leaseStorage) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	namespace := genericapirequest.NamespaceValue(ctx)
	key := objcetKey{Name: name, Namespace: namespace}

	l.RLock()
	obj, ok := l.store[key]
	l.RUnlock()
	if !ok {
		return nil, errors.NewNotFound(coordinationv1.Resource("leases"), fmt.Sprintf("%s/%s", namespace, name))
	}
	return obj, nil
}

func (l *leaseStorage) notifyWatchers(eventType watch.EventType, obj runtime.Object) {
	var deleted []*watch.FakeWatcher
	l.RLock()
	for watcher := range l.watches {
		if watcher.IsStopped() {
			deleted = append(deleted, watcher)
		} else {
			watcher.Action(eventType, obj)
		}
	}
	l.RUnlock()

	l.Lock()
	for _, key := range deleted {
		delete(l.watches, key)
	}
	l.Unlock()
}

func (l *leaseStorage) Watch(ctx context.Context, options *metainternalversion.ListOptions) (watch.Interface, error) {
	ch := watch.NewFake()

	go func() {
		l.Lock()
		l.watches[ch] = struct{}{}
		l.Unlock()

		items := l.list(ctx, options)
		for _, item := range items.Items {
			ch.Add(&item)
		}
	}()

	return ch, nil
}

func (*leaseStorage) NewList() runtime.Object {
	return &coordinationv1.LeaseList{}
}

func (l *leaseStorage) List(ctx context.Context, options *metainternalversion.ListOptions) (runtime.Object, error) {
	items := l.list(ctx, options)

	sort.Slice(items.Items, func(i, j int) bool {
		if items.Items[i].Namespace != items.Items[j].Namespace {
			return items.Items[i].Namespace < items.Items[j].Namespace
		}
		return items.Items[i].Name < items.Items[j].Name
	})
	return items, nil
}

func (l *leaseStorage) list(ctx context.Context, options *metainternalversion.ListOptions) *coordinationv1.LeaseList {
	namespace := genericapirequest.NamespaceValue(ctx)

	l.RLock()
	defer l.RUnlock()
	var items coordinationv1.LeaseList
	for key, obj := range l.store {
		if namespace != "" && key.Namespace != namespace {
			continue
		}
		if options.LabelSelector != nil && !options.LabelSelector.Matches(labels.Set(obj.Labels)) {
			continue
		}

		items.Items = append(items.Items, *obj)
	}

	return &items
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
