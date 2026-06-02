package services

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestWaitForResourceReady_ReturnsImmediatelyWhenAlreadyReady verifies that
// WaitForResourceReady returns immediately when the gate has already been signalled.
func TestWaitForResourceReady_ReturnsImmediatelyWhenAlreadyReady(t *testing.T) {
	svc := &K8sResoureService{
		logger: &mockNebulaLogger{},
	}
	svc.markResourceReady()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	err := svc.WaitForResourceReady(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("WaitForResourceReady should return immediately when already ready, took %v", elapsed)
	}
}

// TestWaitForResourceReady_BlocksUntilSignalled verifies that WaitForResourceReady
// blocks until markResourceReady is called, then returns successfully.
func TestWaitForResourceReady_BlocksUntilSignalled(t *testing.T) {
	svc := &K8sResoureService{
		logger: &mockNebulaLogger{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var unblocked atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := svc.WaitForResourceReady(ctx)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		unblocked.Store(true)
	}()

	// Give the goroutine time to block on the gate
	time.Sleep(100 * time.Millisecond)
	if unblocked.Load() {
		t.Fatal("WaitForResourceReady should have blocked before markResourceReady")
	}

	// Signal readiness
	svc.markResourceReady()

	// Wait for unblock
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForResourceReady did not unblock after markResourceReady")
	}
}

// TestWaitForResourceReady_ContextCancellation verifies that WaitForResourceReady
// returns context.Canceled when the context is cancelled before the gate is signalled.
func TestWaitForResourceReady_ContextCancellation(t *testing.T) {
	svc := &K8sResoureService{
		logger: &mockNebulaLogger{},
	}

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.WaitForResourceReady(ctx)
	}()

	// Cancel context before signalling
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForResourceReady did not return after context cancellation")
	}
}

// TestWaitForResourceReady_ContextDeadlineExceeded verifies that WaitForResourceReady
// returns context.DeadlineExceeded when the context deadline passes before the gate is signalled.
func TestWaitForResourceReady_ContextDeadlineExceeded(t *testing.T) {
	svc := &K8sResoureService{
		logger: &mockNebulaLogger{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.WaitForResourceReady(ctx)
	}()

	select {
	case err := <-errCh:
		if err != context.DeadlineExceeded {
			t.Errorf("expected context.DeadlineExceeded, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForResourceReady did not return after context deadline")
	}
}

// TestWaitForResourceReady_MultipleWaitersAllUnblock verifies that multiple
// concurrent waiters all unblock when markResourceReady is called once.
func TestWaitForResourceReady_MultipleWaitersAllUnblock(t *testing.T) {
	svc := &K8sResoureService{
		logger: &mockNebulaLogger{},
	}

	numWaiters := 10
	var wg sync.WaitGroup
	wg.Add(numWaiters)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	successCount := int32(0)
	for i := 0; i < numWaiters; i++ {
		go func() {
			defer wg.Done()
			err := svc.WaitForResourceReady(ctx)
			if err == nil {
				atomic.AddInt32(&successCount, 1)
			}
		}()
	}

	// Give goroutines time to block
	time.Sleep(100 * time.Millisecond)

	// Signal once
	svc.markResourceReady()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		count := atomic.LoadInt32(&successCount)
		if count != int32(numWaiters) {
			t.Errorf("expected %d waiters to succeed, got %d", numWaiters, count)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Not all waiters unblocked within timeout")
	}
}

// TestMarkResourceReady_Idempotent verifies that calling markResourceReady
// multiple times is safe (sync.Once semantics).
func TestMarkResourceReady_Idempotent(t *testing.T) {
	svc := &K8sResoureService{
		logger: &mockNebulaLogger{},
	}

	// Call markResourceReady multiple times — should not panic
	svc.markResourceReady()
	svc.markResourceReady()
	svc.markResourceReady()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := svc.WaitForResourceReady(ctx)
	if err != nil {
		t.Fatalf("unexpected error after multiple markResourceReady calls: %v", err)
	}
}
