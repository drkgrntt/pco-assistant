package scheduling

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestRunConcurrentlyCallsFnForEveryItem(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}

	var mu sync.Mutex
	var seen []int
	RunConcurrently(items, func(i int) {
		mu.Lock()
		seen = append(seen, i)
		mu.Unlock()
	})

	if len(seen) != len(items) {
		t.Fatalf("expected %d calls, got %d", len(items), len(seen))
	}
	for _, item := range items {
		found := false
		for _, s := range seen {
			if s == item {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected item %d to have been passed to fn", item)
		}
	}
}

func TestRunConcurrentlyRespectsFanOutLimit(t *testing.T) {
	items := make([]int, PCOFanOutLimit*3)
	for i := range items {
		items[i] = i
	}

	var current, max int64
	release := make(chan struct{})
	var startedOnce sync.Once
	started := make(chan struct{})

	runConcurrentlyDone := make(chan struct{})
	go func() {
		RunConcurrently(items, func(i int) {
			n := atomic.AddInt64(&current, 1)
			for {
				old := atomic.LoadInt64(&max)
				if n <= old || atomic.CompareAndSwapInt64(&max, old, n) {
					break
				}
			}
			startedOnce.Do(func() { close(started) })
			<-release
			atomic.AddInt64(&current, -1)
		})
		close(runConcurrentlyDone)
	}()

	<-started
	close(release)
	<-runConcurrentlyDone

	if max > PCOFanOutLimit {
		t.Errorf("expected at most %d concurrent calls, saw %d", PCOFanOutLimit, max)
	}
}

func TestRunConcurrentlyEmptyItemsNoOp(t *testing.T) {
	called := false
	RunConcurrently([]int{}, func(int) { called = true })
	if called {
		t.Error("expected fn not to be called for an empty items slice")
	}
}
