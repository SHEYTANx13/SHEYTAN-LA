// test_scheduler.cpp — Phase 4 scheduler tests (dependency-free asserts).
//
// Verifies the bounded scheduler: submit, cancel, pop_next FIFO ordering,
// drain on shutdown, queue-full rejection, stats accuracy, and graceful
// shutdown behavior.

#include "shtn/engine.h"
#include "shtn/types.h"

#include "scheduler.h"

#include <atomic>
#include <cstdio>
#include <cstring>
#include <memory>
#include <string>

static int failures = 0;

#define CHECK(cond)                                                        \
    do {                                                                   \
        if (!(cond)) {                                                     \
            std::fprintf(stderr, "FAIL %s:%d: %s\n", __FILE__, __LINE__,    \
                         #cond);                                           \
            ++failures;                                                    \
        }                                                                  \
    } while (0)

int main() {
    using namespace shtn::sched;

    // --- submit + pop_next FIFO ordering -------------------------------
    {
        Scheduler s(8);
        std::string err;

        auto r1 = std::make_shared<Request>("req-1", nullptr);
        auto r2 = std::make_shared<Request>("req-2", nullptr);
        auto r3 = std::make_shared<Request>("req-3", nullptr);

        CHECK(s.submit(r1, err) == SHTN_OK);
        CHECK(s.submit(r2, err) == SHTN_OK);
        CHECK(s.submit(r3, err) == SHTN_OK);

        Stats st = s.stats();
        CHECK(st.queued_requests == 3);
        CHECK(st.total_submitted == 3);

        // Pop FIFO.
        RequestPtr p1 = s.pop_next();
        CHECK(p1 != nullptr);
        CHECK(p1->id == "req-1");
        CHECK(p1->state.load() == State::kActive);

        RequestPtr p2 = s.pop_next();
        CHECK(p2 != nullptr);
        CHECK(p2->id == "req-2");

        RequestPtr p3 = s.pop_next();
        CHECK(p3 != nullptr);
        CHECK(p3->id == "req-3");

        RequestPtr p4 = s.pop_next();
        CHECK(p4 == nullptr); // queue empty
    }

    // --- queue full rejection ------------------------------------------
    {
        Scheduler s(2); // depth 2
        std::string err;

        auto r1 = std::make_shared<Request>("a", nullptr);
        auto r2 = std::make_shared<Request>("b", nullptr);
        auto r3 = std::make_shared<Request>("c", nullptr);

        CHECK(s.submit(r1, err) == SHTN_OK);
        CHECK(s.submit(r2, err) == SHTN_OK);
        CHECK(s.submit(r3, err) == SHTN_ERR_UNSUPPORTED); // queue full

        Stats st = s.stats();
        CHECK(st.queued_requests == 2);
        CHECK(st.queue_depth_limit == 2);
    }

    // --- cancel a queued request ---------------------------------------
    {
        Scheduler s(8);
        std::string err;

        auto r1 = std::make_shared<Request>("x", nullptr);
        auto r2 = std::make_shared<Request>("y", nullptr);

        CHECK(s.submit(r1, err) == SHTN_OK);
        CHECK(s.submit(r2, err) == SHTN_OK);

        CHECK(s.cancel("x") == true);
        CHECK(r1->state.load() == State::kCancelled);

        Stats st = s.stats();
        CHECK(st.queued_requests == 1);
        CHECK(st.total_cancelled == 1);

        // Pop returns the surviving request.
        RequestPtr p = s.pop_next();
        CHECK(p != nullptr);
        CHECK(p->id == "y");

        // Cancel a non-existent id.
        CHECK(s.cancel("nope") == false);
    }

    // --- cancel an already-terminal request is a no-op -----------------
    {
        Scheduler s(8);
        std::string err;

        auto r1 = std::make_shared<Request>("x", nullptr);
        CHECK(s.submit(r1, err) == SHTN_OK);
        CHECK(s.cancel("x") == true);

        // Second cancel of the same id: not in queue, not active → false.
        CHECK(s.cancel("x") == false);
    }

    // --- invalid argument rejection ------------------------------------
    {
        Scheduler s(8);
        std::string err;

        CHECK(s.submit(nullptr, err) == SHTN_ERR_INVALID_ARG);

        auto r1 = std::make_shared<Request>("", nullptr); // empty id
        CHECK(s.submit(r1, err) == SHTN_ERR_INVALID_ARG);
    }

    // --- shutdown drains the queue -------------------------------------
    {
        Scheduler s(8);
        std::string err;

        auto r1 = std::make_shared<Request>("a", nullptr);
        auto r2 = std::make_shared<Request>("b", nullptr);
        auto r3 = std::make_shared<Request>("c", nullptr);

        CHECK(s.submit(r1, err) == SHTN_OK);
        CHECK(s.submit(r2, err) == SHTN_OK);
        CHECK(s.submit(r3, err) == SHTN_OK);

        s.shutdown();

        Stats st = s.stats();
        CHECK(st.shutting_down == true);
        CHECK(st.queued_requests == 0);
        CHECK(st.total_cancelled == 3);

        // Submit after shutdown fails.
        auto r4 = std::make_shared<Request>("d", nullptr);
        CHECK(s.submit(r4, err) == SHTN_ERR_MODEL_STATE);
    }

    // --- queue depth limit clamped to kMaxQueueDepth -------------------
    {
        Scheduler s(1000); // ask for too much
        Stats st = s.stats();
        CHECK(st.queue_depth_limit == kMaxQueueDepth);
    }

    // --- default queue depth -------------------------------------------
    {
        Scheduler s; // default
        Stats st = s.stats();
        CHECK(st.queue_depth_limit == kDefaultQueueDepth);
        CHECK(st.max_concurrent == kMaxConcurrent);
        CHECK(st.max_concurrent == 1); // single-slot
    }

    // --- pop_next skips already-cancelled ------------------------------
    {
        Scheduler s(8);
        std::string err;

        auto r1 = std::make_shared<Request>("a", nullptr);
        auto r2 = std::make_shared<Request>("b", nullptr);
        auto r3 = std::make_shared<Request>("c", nullptr);

        CHECK(s.submit(r1, err) == SHTN_OK);
        CHECK(s.submit(r2, err) == SHTN_OK);
        CHECK(s.submit(r3, err) == SHTN_OK);

        // Cancel r2 while still queued.
        CHECK(s.cancel("b") == true);

        // pop_next returns r1 then r3 (skips r2).
        RequestPtr p1 = s.pop_next();
        CHECK(p1->id == "a");
        RequestPtr p2 = s.pop_next();
        CHECK(p2->id == "c");
        RequestPtr p3 = s.pop_next();
        CHECK(p3 == nullptr);
    }

    if (failures > 0) {
        std::fprintf(stderr, "test_scheduler: %d failure(s)\n", failures);
        return 1;
    }

    std::printf("test_scheduler: all checks passed\n");
    return 0;
}
