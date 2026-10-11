// scheduler.h — the SHEYTAN native engine bounded scheduler (Phase 4 → 5).
//
// A real bounded single-slot scheduler with cancellation, fair FIFO
// ordering, graceful shutdown and no busy polling.
//
// Phase 5 upgrade: the scheduler can run a REAL worker thread
// (start_worker) that pops queued requests and executes them one at a
// time (single-slot execution, no continuous batching). When no worker is
// installed the scheduler behaves exactly like Phase 4 (bounded queue,
// measurable, executes nothing) — the Phase 4 tests pin that behaviour.
//
// What this is:
//   - a real bounded queue (capacity = queue_depth_limit, clamped);
//   - real cancellation: queued requests are removed and marked
//     cancelled; the ACTIVE request gets its cancel_requested flag set
//     (the generation loop observes it at every token) and the execution
//     itself decides the terminal transition;
//   - fair FIFO ordering (deque, push back / pop front);
//   - real active/completed/cancelled/failed counters when the worker
//     runs (measured, never artificial);
//   - graceful shutdown (drains pending requests with a "shutting down"
//     cancellation before destruction; the active request is cancelled
//     by flag and joined);
//   - no busy polling (the worker blocks on a condition variable).
//
// What this is NOT:
//   - this is NOT continuous batching (one request at a time);
//   - this is NOT speculative.

#ifndef SHTN_SCHEDULER_H
#define SHTN_SCHEDULER_H

#include <atomic>
#include <condition_variable>
#include <cstdint>
#include <deque>
#include <functional>
#include <memory>
#include <mutex>
#include <string>
#include <thread>

namespace shtn {
namespace sched {

// Hard bounds.
constexpr uint32_t kMaxConcurrent = 1;          // single-slot execution
constexpr uint32_t kDefaultQueueDepth = 8;      // bounded queue
constexpr uint32_t kMaxQueueDepth = 64;         // hard cap

// RequestState mirrors the lifecycle a scheduled request goes through.
enum class State : uint8_t {
    kQueued     = 0, // waiting for the execution slot
    kActive     = 1, // executing on the worker (when one is installed)
    kCancelled  = 2, // cancelled before or during execution
    kCompleted  = 3, // finished successfully
    kFailed     = 4, // finished with an error
};

// Request is one scheduled unit. The generation path creates one of
// these (id + a way to observe cancellation + a way to wait for the
// terminal state) and hands it to the scheduler; the installed worker
// executor performs the actual work and MUST call finish() exactly once
// with the outcome.
struct Request : public std::enable_shared_from_this<Request> {
    std::string id;
    std::function<void()> execute;   // Phase 4 compatibility (unused by
                                     // the Phase 5 engine worker)
    std::atomic<State> state{State::kQueued};
    std::atomic<bool> cancel_requested{false};

    Request() = default;
    explicit Request(std::string id_, std::function<void()> exec)
        : id(std::move(id_)), execute(std::move(exec)) {}

    // --- completion signaling (used by the generation submitter) -------
    void wait_terminal();
    bool is_terminal() const {
        State s = state.load(std::memory_order_acquire);
        return s == State::kCancelled || s == State::kCompleted ||
               s == State::kFailed;
    }

private:
    friend class Scheduler;
    // Notified by Scheduler::finish under the scheduler mutex.
    std::mutex terminal_mu;
    std::condition_variable terminal_cv;
};

using RequestPtr = std::shared_ptr<Request>;

// Stats is the measurable scheduler snapshot reported through metrics.
struct Stats {
    uint32_t active_requests = 0;       // currently executing (measured)
    uint32_t queued_requests = 0;       // waiting in the queue
    uint32_t max_concurrent = kMaxConcurrent;
    uint32_t queue_depth_limit = kDefaultQueueDepth;
    uint64_t total_submitted = 0;       // cumulative since scheduler create
    uint64_t total_completed = 0;
    uint64_t total_cancelled = 0;
    uint64_t total_failed = 0;
    bool shutting_down = false;
};

// Executor is the worker's execution hook: it performs the request's
// work and RETURNS the outcome (SHTN_OK / SHTN_ERR_CANCELLED / other
// error codes). The scheduler records the terminal transition itself.
using Executor = std::function<int32_t(RequestPtr)>;

// Scheduler is the bounded request queue + optional worker. Thread-safe.
class Scheduler {
public:
    explicit Scheduler(uint32_t queue_depth = kDefaultQueueDepth);
    ~Scheduler();

    Scheduler(const Scheduler&) = delete;
    Scheduler& operator=(const Scheduler&) = delete;

    // start_worker installs the REAL execution function and launches the
    // single worker thread. Idempotent: a second call while a worker runs
    // returns SHTN_ERR_MODEL_STATE. Without a worker the scheduler keeps
    // the exact Phase 4 behaviour (queue only).
    int32_t start_worker(Executor exec);

    // Submit enqueues a request. Returns:
    //   SHTN_OK                enqueued;
    //   SHTN_ERR_INVALID_ARG   id empty or request null;
    //   SHTN_ERR_MODEL_STATE   scheduler is shutting down;
    //   SHTN_ERR_UNSUPPORTED   queue is full.
    int32_t submit(RequestPtr req, std::string& error);

    // Cancel a request. A QUEUED request is removed and marked cancelled.
    // The ACTIVE request (worker installed) gets cancel_requested = true
    // — the execution loop observes it and finishes with
    // SHTN_ERR_CANCELLED; cancel() returns true immediately.
    // Returns false when no request with this id is queued or active.
    bool cancel(const std::string& id);

    // finish records the terminal outcome of an ACTIVE request (called
    // exactly once by the executor or by drain of an active request at
    // shutdown). rc == SHTN_OK → completed; rc == SHTN_ERR_CANCELLED →
    // cancelled; otherwise failed. Wakes the submitter's wait_terminal.
    void finish(RequestPtr req, int32_t rc);

    // Pop the next non-cancelled request from the queue (FIFO). Returns
    // nullptr when the queue is empty. Direct use is for the Phase 4
    // behaviour / tests; the installed worker calls it internally.
    RequestPtr pop_next();

    // Drain cancels every queued request. Used by Shutdown and the
    // destructor. The active request (if any) is cancelled by flag and
    // the worker is joined by shutdown().
    void drain();

    // Shutdown marks the scheduler as shutting down, drains the queue,
    // flags the active request cancelled and joins the worker thread.
    // After shutdown, submit returns SHTN_ERR_MODEL_STATE.
    void shutdown();

    // Stats returns the measurable snapshot (thread-safe).
    Stats stats() const;

    // Is the scheduler accepting new requests?
    bool accepting() const;

    // Is a worker installed and running?
    bool worker_running() const;

private:
    void worker_loop();

    mutable std::mutex mu_;
    std::condition_variable cv_;
    std::deque<RequestPtr> queue_;
    uint32_t queue_depth_limit_;
    bool shutting_down_ = false;

    // Worker state.
    Executor executor_;                 // guarded by mu_
    std::thread worker_;                // launched by start_worker
    bool worker_started_ = false;       // guarded by mu_
    RequestPtr active_;                 // guarded by mu_
    uint32_t active_count_ = 0;         // 0/1 single slot (guarded by mu_)

    std::atomic<uint64_t> total_submitted_{0};
    std::atomic<uint64_t> total_completed_{0};
    std::atomic<uint64_t> total_cancelled_{0};
    std::atomic<uint64_t> total_failed_{0};
};

} // namespace sched
} // namespace shtn

#endif /* SHTN_SCHEDULER_H */
