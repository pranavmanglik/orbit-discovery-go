// Copyright 2026-present Orbit Contributors.
// Licensed under the Apache License, Version 2.0.

package discovery_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/orbit-projects/orbit-discovery-go/discovery"
)

func TestWeightedPickerUsesWeightsAndReturnsDetachedSnapshots(t *testing.T) {
	instances := []discovery.Instance{
		{Service: "catalog", InstanceID: "primary", Host: "BÜCHER.de.", Port: 8081, Scheme: "http", Weight: 3,
			Metadata: map[string]string{"zone": "west"}},
		{Service: "catalog", InstanceID: "secondary", Host: "127.0.0.1", Port: 8082, Scheme: "http", Weight: 1},
	}
	picker, err := discovery.NewWeightedPicker("catalog", instances)
	if err != nil {
		t.Fatal(err)
	}
	instances[0].Metadata["zone"] = "mutated"
	counts := map[string]int{}
	for range 4 {
		instance, err := picker.Pick()
		if err != nil {
			t.Fatal(err)
		}
		counts[instance.InstanceID]++
		if instance.Host != "xn--bcher-kva.de" && instance.InstanceID == "primary" {
			t.Fatalf("host was not normalized: %q", instance.Host)
		}
		if instance.InstanceID == "primary" {
			if instance.Metadata["zone"] != "west" {
				t.Fatalf("picker retained caller mutation: %#v", instance.Metadata)
			}
			instance.Metadata["zone"] = "response mutation"
		}
	}
	if counts["primary"] != 3 || counts["secondary"] != 1 {
		t.Fatalf("unexpected weighted distribution: %#v", counts)
	}
	selected, err := picker.Pick()
	if err != nil {
		t.Fatal(err)
	}
	if selected.Metadata["zone"] != "west" {
		t.Fatalf("returned metadata mutation affected picker state: %#v", selected.Metadata)
	}
}

func TestWeightedPickerUpdateIsAtomicAndReportsEmptySnapshot(t *testing.T) {
	picker, err := discovery.NewWeightedPicker("catalog", []discovery.Instance{{
		Service: "catalog", InstanceID: "ready", Host: "localhost", Port: 8080, Scheme: "http", Weight: 1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	invalid := []discovery.Instance{{
		Service: "other", InstanceID: "bad", Host: "localhost", Port: 8080, Scheme: "http", Weight: 1,
	}}
	if err := picker.Update(invalid); err == nil {
		t.Fatal("expected wrong-service snapshot to be rejected")
	}
	current, err := picker.Pick()
	if err != nil || current.InstanceID != "ready" {
		t.Fatalf("failed update replaced current snapshot: %#v, %v", current, err)
	}
	if err := picker.Update(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := picker.Pick(); !errors.Is(err, discovery.ErrNoInstances) {
		t.Fatalf("empty snapshot should return ErrNoInstances, got %v", err)
	}
}

func TestWeightedPickerConcurrentCallsRemainSafe(t *testing.T) {
	instances := []discovery.Instance{
		{Service: "catalog", InstanceID: "one", Host: "localhost", Port: 8081, Scheme: "http", Weight: 1},
		{Service: "catalog", InstanceID: "two", Host: "localhost", Port: 8082, Scheme: "http", Weight: 2},
	}
	picker, err := discovery.NewWeightedPicker("catalog", instances)
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	errorsFound := make(chan error, 8)
	for worker := range 8 {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for call := range 100 {
				instance, err := picker.Pick()
				if err != nil {
					errorsFound <- err
					return
				}
				if instance.Service != "catalog" {
					errorsFound <- fmt.Errorf("worker %d call %d selected unexpected service", worker, call)
					return
				}
			}
		}(worker)
	}
	workers.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatal(err)
	}
}
