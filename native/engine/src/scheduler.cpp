// scheduler.cpp — native engine bounded scheduler implementation
// (Phase 4 → Phase 5: real single-slot worker execution).

#include "scheduler.h"

#include "shtn/engine.h"

namespace shtn {
namespace sched {

Scheduler::Scheduler(uint32_t queue_depth)
    : queue_depth_limit_(queue_depth == 0 ? kDefaultQueueDepth
                       : queue_depth > kMaxQueueDepth ? kMaxQueueDepth
                                                      : queue_depth) {}

Scheduler::~Scheduler() {
    shutdown();
}

int32_t Scheduler::start_worker(Executor exec) {
    if (!exec) {
        return SHTN_ERR_INVALID_ARG;
    }

    std::lock_guard<std::mutex> lock(mu_);
    if (worker_started_) {
        return SHTN_ERR_MODEL_STATE;
    }
    if (shutting_down_) {
        return SHTN_ERR_MODEL_STATE;
    }

    executor_ = std::move(exec);
    worker_started_ = true;
    worker_ = std::thread([this] { worker_loop(); });

    return SHTN_OK;
}

bool Scheduler::worker_running() const {
    std::lock_guard<std::mutex> lock(mu_);
    return worker_started_ && !shutting_down_;
}

int32_t Scheduler::submit(RequestPtr req, std::string& error) {
    if (!req) {
        error = "scheduler: null request";
        return SHTN_ERR_INVALID_ARG;
    }
    if (req->id.empty()) {
        error = "scheduler: empty request id";
        return SHTN_ERR_INVALID_ARG;
    }

    {
        std::lock_guard<std::mutex> lock(mu_);

        if (shutting_down_) {
            error = "scheduler: shutting down";
            return SHTN_ERR_MODEL_STATE;
        }

        if (queue_.size() >= queue_depth_limit_) {
            error = "scheduler: queue is full (" +
                    std::to_string(queue_depth_limit_) + ")";
            return SHTN_ERR_UNSUPPORTED;
        }

        queue_.push_back(req);
        total_submitted_.fetch_add(1, std::memory_order_relaxed);
    }
    cv_.notify_one();

    return SHTN_OK;
}

bool Scheduler::cancel(const std::string& id) {
    if (id.empty()) {
        return false;
    }

    std::lock_guard<std::mutex> lock(mu_);

    for (auto it = queue_.begin(); it != queue_.end(); ++it) {
        RequestPtr& r = *it;
        if (r && r->id == id) {
            // Still queued: remove and mark cancelled.
            State expected = State::kQueued;
            if (r->state.compare_exchange_strong(expected,
                                                  State::kCancelled)) {
                queue_.erase(it);
                total_cancelled_.fetch_add(1, std::memory_order_relaxed);
                {
                    std::lock_guard<std::mutex> tm(r->terminal_mu);
                    r->terminal_cv.notify_all();
                }
                return true;
            }
            // Already terminal — leave it; return false.
            return false;
        }
    }

    // Active request: set the cooperative cancel flag; the execution
    // loop observes it (every token in generation) and finishes with
    // SHTN_ERR_CANCELLED. This returns true immediately — the flag is
    // the signal, not the completion.
    if (active_ && active_->id == id &&
        active_->state.load(std::memory_order_acquire) == State::kActive) {
        active_->cancel_requested.store(true, std::memory_order_release);
        return true;
    }

    return false;
}

void Scheduler::finish(RequestPtr req, int32_t rc) {
    if (!req) {
        return;
    }

    {
        std::lock_guard<std::mutex> lock(mu_);
        if (active_ == req) {
            active_ = nullptr;
            active_count_ = 0;
        }
    }

    State terminal = State::kFailed;
    if (rc == SHTN_OK) {
        terminal = State::kCompleted;
        total_completed_.fetch_add(1, std::memory_order_relaxed);
    } else if (rc == SHTN_ERR_CANCELLED) {
        terminal = State::kCancelled;
        total_cancelled_.fetch_add(1, std::memory_order_relaxed);
    } else {
        terminal = State::kFailed;
        total_failed_.fetch_add(1, std::memory_order_relaxed);
    }

    // Only the executor transitions ACTIVE → terminal; a queued-cancelled
    // request is already terminal.
    State expected = State::kActive;
    req->state.compare_exchange_strong(expected, terminal);

    {
        std::lock_guard<std::mutex> tm(req->terminal_mu);
        req->terminal_cv.notify_all();
    }
}

void Scheduler::worker_loop() {
    for (;;) {
        RequestPtr req;

        {
            std::unique_lock<std::mutex> lock(mu_);

            cv_.wait(lock, [this] {
                return shutting_down_ || !queue_.empty();
            });

            if (shutting_down_ && queue_.empty()) {
                return;
            }

            // Pop the next runnable request (skips cancelled).
            while (!queue_.empty()) {
                RequestPtr r = queue_.front();
                queue_.pop_front();

                if (!r) {
                    continue;
                }

                State s = r->state.load(std::memory_order_relaxed);
                if (s == State::kCancelled) {
                    continue;
                }

                State expected = State::kQueued;
                if (r->state.compare_exchange_strong(expected,
                                                     State::kActive)) {
                    req = r;
                    break;
                }
            }

            if (req == nullptr) {
                if (shutting_down_) {
                    return;
                }
                continue;
            }

            active_ = req;
            active_count_ = 1;
        }

        // Execute outside the scheduler mutex. The executor MUST call
        // finish(req, rc) exactly once (the engine's generation path
        // does — including on its own error paths).
        int32_t rc = SHTN_ERR_INTERNAL;
        Executor exec;
        {
            std::lock_guard<std::mutex> lock(mu_);
            exec = executor_;
        }
        if (exec) {
            rc = exec(req);
        }

        finish(req, rc);
    }
}

RequestPtr Scheduler::pop_next() {
    std::lock_guard<std::mutex> lock(mu_);

    while (!queue_.empty()) {
        RequestPtr r = queue_.front();
        queue_.pop_front();

        if (!r) {
            continue;
        }

        // Skip already-cancelled requests.
        State s = r->state.load(std::memory_order_relaxed);
        if (s == State::kCancelled) {
            continue;
        }

        // Mark active.
        State expected = State::kQueued;
        if (r->state.compare_exchange_strong(expected, State::kActive)) {
            return r;
        }
    }

    return nullptr;
}

void Scheduler::drain() {
    std::lock_guard<std::mutex> lock(mu_);

    while (!queue_.empty()) {
        RequestPtr r = queue_.front();
        queue_.pop_front();
        if (r) {
            State expected = State::kQueued;
            if (r->state.compare_exchange_strong(expected,
                                                  State::kCancelled)) {
                total_cancelled_.fetch_add(1, std::memory_order_relaxed);
            }
            {
                std::lock_guard<std::mutex> tm(r->terminal_mu);
                r->terminal_cv.notify_all();
            }
        }
    }
}

void Scheduler::shutdown() {
    {
        std::lock_guard<std::mutex> lock(mu_);
        shutting_down_ = true;
        // Signal the active request's cooperative cancellation.
        if (active_) {
            active_->cancel_requested.store(true, std::memory_order_release);
        }
    }
    drain();
    cv_.notify_all();

    // Join the worker (it exits once the queue is drained and the active
    // request finished — the executor observes the cancel flag).
    if (worker_.joinable()) {
        worker_.join();
    }

    {
        std::lock_guard<std::mutex> lock(mu_);
        // If an active request never finished (misbehaving executor),
        // normalize it now so no submitter waits forever.
        if (active_) {
            RequestPtr r = active_;
            active_ = nullptr;
            active_count_ = 0;
            State expected = State::kActive;
            if (r->state.compare_exchange_strong(expected,
                                                  State::kCancelled)) {
                total_cancelled_.fetch_add(1, std::memory_order_relaxed);
            }
            std::lock_guard<std::mutex> tm(r->terminal_mu);
            r->terminal_cv.notify_all();
        }
    }
}

Stats Scheduler::stats() const {
    std::lock_guard<std::mutex> lock(mu_);

    Stats s;
    s.active_requests = active_count_;
    s.queued_requests = static_cast<uint32_t>(queue_.size());
    s.max_concurrent = kMaxConcurrent;
    s.queue_depth_limit = queue_depth_limit_;
    s.total_submitted = total_submitted_.load(std::memory_order_relaxed);
    s.total_completed = total_completed_.load(std::memory_order_relaxed);
    s.total_cancelled = total_cancelled_.load(std::memory_order_relaxed);
    s.total_failed = total_failed_.load(std::memory_order_relaxed);
    s.shutting_down = shutting_down_;

    return s;
}

bool Scheduler::accepting() const {
    std::lock_guard<std::mutex> lock(mu_);
    return !shutting_down_;
}

void Request::wait_terminal() {
    std::unique_lock<std::mutex> lock(terminal_mu);
    terminal_cv.wait(lock, [this] { return is_terminal(); });
}

} // namespace sched
} // namespace shtn
