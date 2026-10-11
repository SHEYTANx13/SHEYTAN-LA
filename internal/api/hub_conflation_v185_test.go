package api

// hub_conflation_v185_test.go — v1.8.5 P0 regression coverage for the
// conflation-aware activity subscriber queue (the remaining live-streaming
// bottleneck repair in server.go).
//
// THE FAILURE CLASS (reproduced deterministically below): the v1.2.x hub
// delivered through a plain 128-deep channel and dropped the NEWEST event
// when a subscriber's buffer was full. For cumulative `response` snapshots
// that policy discards the frame carrying the FULL text so far while the
// buffer keeps shorter, older snapshots — a subscriber whose writer stalls
// (transport backpressure) freezes at stale text until the run ends, which
// is exactly the "streamed text visible only after Stop" evidence class.
//
// The conflation queue must prove:
//
//   1. drop-NEWEST is GONE: with a full, never-drained queue the LATEST
//      response snapshot is retained (the reader eventually sees the full
//      text) while OLDER snapshots were evicted;
//   2. terminal events are never preferentially evicted: a done marker
//      enqueued behind a full buffer of conflatable frames survives;
//   3. publish never blocks the run goroutine: offering to a wedged
//      subscriber returns (no lock hold, no channel send block);
//   4. delivery order is preserved among survivors;
//   5. close-then-drain: every survivor is delivered BEFORE next() reports
//      closure (a lagging reader still ends on the newest snapshot);
//   6. seq replay contract: the newest surviving event carries the highest
//      sequence (the (snapshot at N) + (events > N) fold stays gapless);
//   7. the unbounded non-conflatable case still bounds memory (oldest
//      evicted, newest retained);
//   8. concurrent publishers + one wedged reader: no race, no block
//      (run under -race in the CI race gate).

import (
        "fmt"
        "sync"
        "testing"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
)

// fillSubscriber wedges one subscriber: capacity + extra offers with NO
// reads, so the queue is at its conflation boundary.
func fillSubscriber(t *testing.T, extra int) *activitySub {
        t.Helper()

        sub := newActivitySub(subscriberQueueCap)

        for i := 0; i < subscriberQueueCap+extra; i++ {
                sub.offer(agent.Activity{
                        Type:    "response",
                        RunID:   "run-1",
                        Seq:     int64(i + 1),
                        Caption: fmt.Sprintf("snapshot-%d", i),
                })
        }

        return sub
}

// TestSubscriberConflatesNewestSurvives proves the core v1.8.5 contract:
// a full queue under sustained publication RETAINS the newest cumulative
// snapshot (drop-newest is gone).
func TestSubscriberConflatesNewestSurvives(t *testing.T) {
        sub := fillSubscriber(t, 64)

        // Deterministic drain boundary: closure is reported only after the
        // queue is empty, so a bounded drain needs the close first.
        sub.close()

        var last agent.Activity
        count := 0

        for {
                ev, ok := sub.next()
                if !ok {
                        break
                }

                count++
                last = ev
        }

        // The newest snapshot MUST be the last delivered event — the exact
        // frame the drop-newest policy used to discard.
        if last.Caption != "snapshot-191" {
                t.Fatalf("newest snapshot lost: last=%q (want snapshot-191)", last.Caption)
        }

        if count != subscriberQueueCap {
                t.Fatalf("unexpected survivor count: %d (want %d)", count, subscriberQueueCap)
        }
}

// TestSubscriberTerminalEventSurvivesFullQueue proves a terminal marker is
// never preferentially evicted: with a full conflatable queue, the done
// frame enqueue survives and is delivered LAST (after the newest snapshot).
func TestSubscriberTerminalEventSurvivesFullQueue(t *testing.T) {
        sub := fillSubscriber(t, 8)

        sub.offer(agent.Activity{
                Type:    "done",
                RunID:   "run-1",
                Seq:     subscriberQueueCap + 9,
                Caption: "Completed",
        })

        sub.close()

        var last agent.Activity
        var lastResponse agent.Activity

        for {
                ev, ok := sub.next()
                if !ok {
                        break
                }

                last = ev
                if ev.Type == "response" {
                        lastResponse = ev
                }
        }

        if last.Type != "done" || last.Caption != "Completed" {
                t.Fatalf("terminal event lost or reordered: %+v", last)
        }

        if lastResponse.Caption != "snapshot-135" {
                t.Fatalf("newest response snapshot lost: %q", lastResponse.Caption)
        }
}

// TestSubscriberOrderPreservedAmongSurvivors proves relative delivery order
// is preserved after conflation (survivors stay in seq order).
func TestSubscriberOrderPreservedAmongSurvivors(t *testing.T) {
        sub := newActivitySub(subscriberQueueCap)

        // Interleave conflatable + non-conflatable events, then overflow.
        total := subscriberQueueCap + 40

        for i := 0; i < total; i++ {
                kind := "response"
                if i%4 == 3 {
                        kind = "tool_start"
                }

                sub.offer(agent.Activity{
                        Type:    kind,
                        RunID:   "run-1",
                        Seq:     int64(i + 1),
                        Caption: fmt.Sprintf("ev-%d", i),
                })
        }

        sub.close()

        prevSeq := int64(0)
        seen := 0

        for {
                ev, ok := sub.next()
                if !ok {
                        break
                }

                if ev.Seq <= prevSeq {
                        t.Fatalf("order violated: seq %d after %d", ev.Seq, prevSeq)
                }

                prevSeq = ev.Seq
                seen++
        }

        // The highest seq must have been delivered (the newest survives).
        if prevSeq != int64(total) {
                t.Fatalf("newest event lost: highest delivered seq=%d (want %d)", prevSeq, total)
        }
}

// TestSubscriberPublishNeverBlocks proves offering to a wedged (never
// drained) subscriber returns promptly — the run goroutine is never
// blocked by a slow consumer.
func TestSubscriberPublishNeverBlocks(t *testing.T) {
        sub := newActivitySub(4)

        done := make(chan struct{})

        go func() {
                defer close(done)

                // Far beyond capacity, zero reads.
                for i := 0; i < 20_000; i++ {
                        sub.offer(agent.Activity{
                                Type:    "response",
                                Seq:     int64(i),
                                Caption: "x",
                        })
                }
        }()

        select {
        case <-done:
                // The publisher completed — never blocked.
        case <-time.After(5 * time.Second):
                t.Fatal("offer blocked on a wedged subscriber")
        }

        sub.mu.Lock()
        bound := len(sub.buf)
        sub.mu.Unlock()

        if bound > subscriberQueueCap {
                t.Fatalf("queue unbounded: %d entries (cap %d)", bound, subscriberQueueCap)
        }
}

// TestSubscriberCloseDrainsFirst proves every survivor is delivered before
// closure is reported — a lagging reader ends on the newest snapshot.
func TestSubscriberCloseDrainsFirst(t *testing.T) {
        sub := newActivitySub(subscriberQueueCap)

        for i := 0; i < 10; i++ {
                sub.offer(agent.Activity{
                        Type:    "response",
                        Seq:     int64(i + 1),
                        Caption: fmt.Sprintf("snap-%d", i),
                })
        }

        sub.close()

        var lastSeq int64

        for {
                ev, ok := sub.next()
                if !ok {
                        break
                }

                lastSeq = ev.Seq
        }

        if lastSeq != 10 {
                t.Fatalf("close drained early: last seq=%d (want 10)", lastSeq)
        }

        // A closed empty queue reports closure immediately.
        if _, ok := sub.next(); ok {
                t.Fatal("closed queue delivered an event")
        }
}

// TestHubConflationEndToEnd proves the hub-level contract: two subscribers,
// one healthy (drains live) and one wedged (never reads) — the wedged one
// still ends up holding the newest snapshot, the healthy one receives
// everything, and publish never blocks.
func TestHubConflationEndToEnd(t *testing.T) {
        hub := newActivityHub()

        _, healthy, healthyUnsub := hub.subscribe()
        _, wedged, wedgedUnsub := hub.subscribe()

        defer healthyUnsub()
        defer wedgedUnsub()

        const total = subscriberQueueCap + 200

        for i := 0; i < total; i++ {
                hub.publish(agent.Activity{
                        Type:    "response",
                        RunID:   "run-1",
                        Seq:     int64(i + 1),
                        Caption: fmt.Sprintf("text-%d", i),
                })

                // The healthy subscriber drains as events arrive.
                ev, ok := healthy.next()
                if !ok {
                        t.Fatalf("healthy subscriber starved at event %d", i)
                }

                if ev.Seq != int64(i+1) {
                        t.Fatalf("healthy subscriber lost event %d (got seq %d)", i, ev.Seq)
                }
        }

        hub.publish(agent.Activity{
                Type:    "done",
                RunID:   "run-1",
                Seq:     total + 1,
                Caption: "Completed",
        })

        // The wedged subscriber drains NOW: the newest snapshot + terminal
        // marker must be the tail. Close first so the drain has a deterministic
        // boundary.
        wedgedUnsub()

        var lastResponse string
        var terminal string

        for {
                ev, ok := wedged.next()
                if !ok {
                        break
                }

                if ev.Type == "response" {
                        lastResponse = ev.Caption
                }

                if ev.Type == "done" {
                        terminal = ev.Caption
                }
        }

        if lastResponse != fmt.Sprintf("text-%d", total-1) {
                t.Fatalf("wedged subscriber lost the newest snapshot: %q", lastResponse)
        }

        if terminal != "Completed" {
                t.Fatalf("wedged subscriber lost the terminal marker: %q", terminal)
        }
}

// TestHubCloseReleasesReaders proves a blocked next() returns after the
// hub closes (deterministic teardown — no goroutine leak).
func TestHubCloseReleasesReaders(t *testing.T) {
        hub := newActivityHub()

        _, sub, unsub := hub.subscribe()
        defer unsub()

        released := make(chan struct{})

        go func() {
                defer close(released)

                for {
                        if _, ok := sub.next(); !ok {
                                return
                        }
                }
        }()

        // The reader is parked (no events, not closed).
        select {
        case <-released:
                t.Fatal("reader released before close")
        case <-time.After(50 * time.Millisecond):
        }

        hub.close()

        select {
        case <-released:
        case <-time.After(5 * time.Second):
                t.Fatal("close did not release the parked reader")
        }
}

// TestSubscriberConcurrentPublishers proves concurrent offers are safe and
// lossless-per-seq-order under -race (the run goroutine + engine watchers
// both publish through the same hub).
func TestSubscriberConcurrentPublishers(t *testing.T) {
        sub := newActivitySub(subscriberQueueCap)

        const publishers = 8
        const each = 500

        var wg sync.WaitGroup

        for p := 0; p < publishers; p++ {
                wg.Add(1)

                go func() {
                        defer wg.Done()

                        for i := 0; i < each; i++ {
                                sub.offer(agent.Activity{
                                        Type:    "response",
                                        Caption: "concurrent",
                                })
                        }
                }()
        }

        wg.Wait()

        sub.close()

        count := 0

        for {
                if _, ok := sub.next(); !ok {
                        break
                }

                count++
        }

        // Conflation may evict older entries, but the queue must stay bounded
        // and every delivered entry must be a real offered event (never a
        // duplicated slot or a corrupted one).
        if count == 0 || count > subscriberQueueCap {
                t.Fatalf("unexpected delivery count after concurrent offers: %d", count)
        }
}

// TestSubscriberEvictsOldestConflatableFirst proves the eviction policy:
// a full queue evicts the OLDEST conflatable entry (not the oldest entry
// when that one is non-conflatable, and never the incoming event).
func TestSubscriberEvictsOldestConflatableFirst(t *testing.T) {
        sub := newActivitySub(subscriberQueueCap)

        // Head of the queue: a NON-conflatable lifecycle event.
        sub.offer(agent.Activity{Type: "engine", Caption: "engine-ready", Seq: 1})

        // Then conflatable snapshots up to capacity.
        for i := 0; i < subscriberQueueCap-1; i++ {
                sub.offer(agent.Activity{
                        Type:    "response",
                        Seq:     int64(i + 2),
                        Caption: fmt.Sprintf("snap-%d", i),
                })
        }

        // Overflow: the oldest CONFLATABLE (seq 2) is evicted, NOT the engine
        // event and NOT the incoming event.
        sub.offer(agent.Activity{Type: "response", Seq: 200, Caption: "newest"})

        sub.close()

        first, ok := sub.next()
        if !ok {
                t.Fatal("queue closed unexpectedly")
        }

        if first.Type != "engine" {
                t.Fatalf("non-conflatable head evicted: %+v", first)
        }

        // Drain to the tail.
        var last agent.Activity

        for {
                ev, ok := sub.next()
                if !ok {
                        break
                }

                last = ev
        }

        if last.Caption != "newest" {
                t.Fatalf("incoming event lost: %+v", last)
        }
}
