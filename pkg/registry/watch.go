package registry

import (
	"sync/atomic"

	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	"k8s.io/apimachinery/pkg/watch"
)

type leaseStorageWatch struct {
	f       *leaseStorage
	stopped atomic.Bool
	ch      chan watch.Event

	namespace string
	options   *metainternalversion.ListOptions
}

func (w *leaseStorageWatch) Stop() {
	w.stopped.Store(true)

	w.f.watchesMut.Lock()
	delete(w.f.watches, w)
	w.f.watchesMut.Unlock()
}

func (w *leaseStorageWatch) IsStopped() bool {
	return w.stopped.Load()
}

func (w *leaseStorageWatch) ResultChan() <-chan watch.Event {
	return w.ch
}
