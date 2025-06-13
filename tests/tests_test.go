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

package tests

import (
	"context"
	"testing"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

func pointer[T any](v T) *T {
	return &v
}

func TestCRUD(t *testing.T) {
	leaseClient := clientset.CoordinationV1().Leases("default")

	// Test Create
	lease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-lease",
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       pointer("test-holder"),
			LeaseDurationSeconds: pointer[int32](30),
			RenewTime:            &metav1.MicroTime{Time: time.Now()},
		},
	}
	_, err := leaseClient.Create(context.TODO(), lease, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create lease: %v", err)
	}

	// Test Get
	gotLease, err := leaseClient.Get(context.TODO(), "test-lease", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get lease: %v", err)
	}
	if gotLease.Spec.HolderIdentity == nil || *gotLease.Spec.HolderIdentity != "test-holder" {
		t.Errorf("Unexpected holder identity: %v", gotLease.Spec.HolderIdentity)
	}

	// Test Update
	updatedLease := gotLease.DeepCopy()
	*updatedLease.Spec.HolderIdentity = "updated-holder"
	_, err = leaseClient.Update(context.TODO(), updatedLease, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Failed to update lease: %v", err)
	}

	// Test Delete
	err = leaseClient.Delete(context.TODO(), "test-lease", metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Failed to delete lease: %v", err)
	}
}

func TestList(t *testing.T) {
	leaseClient := clientset.CoordinationV1().Leases("default")

	// Create test leases
	leases := []*coordinationv1.Lease{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-lease-1",
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity: pointer("holder-1"),
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-lease-2",
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity: pointer("holder-2"),
			},
		},
	}

	for _, lease := range leases {
		_, err := leaseClient.Create(context.TODO(), lease, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create lease: %v", err)
		}
	}

	// Test List
	list, err := leaseClient.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list leases: %v", err)
	}

	if len(list.Items) < 2 {
		t.Errorf("Expected at least 2 leases, got %d", len(list.Items))
	}

	// Cleanup
	for _, lease := range leases {
		err = leaseClient.Delete(context.TODO(), lease.Name, metav1.DeleteOptions{})
		if err != nil {
			t.Logf("Failed to delete lease %s: %v", lease.Name, err)
		}
	}
}

func TestWatch(t *testing.T) {
	leaseClient := clientset.CoordinationV1().Leases("default")

	// Create test lease
	lease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-watch-lease",
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity: pointer("test-watch-holder"),
		},
	}

	// Start watching before creation to catch all events
	watcher, err := leaseClient.Watch(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}
	defer watcher.Stop()

	// Create lease
	_, err = leaseClient.Create(context.TODO(), lease, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create lease: %v", err)
	}

	// Update lease
	updatedLease := lease.DeepCopy()
	*updatedLease.Spec.HolderIdentity = "updated-watch-holder"
	_, err = leaseClient.Update(context.TODO(), updatedLease, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Failed to update lease: %v", err)
	}

	// Delete lease
	err = leaseClient.Delete(context.TODO(), lease.Name, metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Failed to delete lease: %v", err)
	}

	// Verify events
	expectedEvents := []watch.EventType{watch.Added, watch.Modified, watch.Deleted}
	receivedEvents := make([]watch.EventType, 0, 3)

	for i := 0; i < 3; i++ {
		select {
		case event := <-watcher.ResultChan():
			receivedEvents = append(receivedEvents, event.Type)
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for watch event")
		}
	}

	if len(receivedEvents) != 3 {
		t.Errorf("Expected 3 watch events, got %d", len(receivedEvents))
	} else {
		for i, eventType := range expectedEvents {
			if receivedEvents[i] != eventType {
				t.Errorf("Expected event %d to be %v, got %v", i, eventType, receivedEvents[i])
			}
		}
	}
}
