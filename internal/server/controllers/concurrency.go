package controllers

import "sync"

// pcoFanOutLimit bounds how many PCO calls a single page-build fans out at
// once via runConcurrently. PCO's rate limit is 100 requests/20s per user
// token (see pco-api-risks.md) - well above what one bounded fan-out inside
// one page render needs, so this is chosen for a solid wall-clock win
// without pushing a burst of near-simultaneous requests at PCO.
const pcoFanOutLimit = 10

// runConcurrently calls fn once per item in items, bounded to at most
// pcoFanOutLimit goroutines running at once, and blocks until every call
// has returned. It exists so the PCO-calling loops in build.go/
// sets_builder.go/rosters_builder.go can fan independent round trips out
// instead of paying each one's ~100-400ms latency serially - see
// pco-api-risks.md's concurrency mitigation for the wall-clock-time
// reasoning (dedup mitigations #1/#2 cut call *count*; this cuts the
// *sequential* chain those calls sit in).
//
// fn is responsible for its own error handling (best-effort, same
// convention as every sequential PCO-calling loop this replaces) and for
// writing its own result somewhere goroutine-safe - either a pre-sized
// slice/array index that's exclusively its own (no lock needed), or a
// shared map guarded by its own mutex (Go maps aren't safe for concurrent
// writes). This helper only manages the fan-out/wait, not result
// collection.
func runConcurrently[T any](items []T, fn func(T)) {
	if len(items) == 0 {
		return
	}

	sem := make(chan struct{}, pcoFanOutLimit)
	var wg sync.WaitGroup
	for _, item := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(item T) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(item)
		}(item)
	}
	wg.Wait()
}
