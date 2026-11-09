// host_main.h — declarations for the shtn-engine-host dispatch host.
//
// The implementation lives ONLY in host_main.cpp, which is compiled
// ONCE and linked into both consumers:
//   * the shtn-engine-host executable (entry point: src/host_entry.cpp);
//   * the native test suite (tests/test_host.cpp drives the loop).
// Tests include THIS header — never the implementation source — so the
// host is not textually duplicated into test translation units (CodeQL
// cpp/include-non-header).
//
// The dispatch-loop rules (one request frame in → one or more response
// frames out; malformed input gets a bounded ERROR response; shutdown
// exits cleanly; stdin EOF cancels in-flight generation) are documented
// at the top of host_main.cpp.

#ifndef SHTN_HOST_MAIN_H
#define SHTN_HOST_MAIN_H

#include <iosfwd>

// shtn_host_run runs the testable host core: it reads length-prefixed
// request frames from `in`, writes framed responses to `out`, and
// returns when the stream ends (EOF) or a shutdown request is served.
// It creates and destroys its own engine instance for the duration of
// the call.
//
// Return: the host exit code — 0 on a clean EOF/shutdown exit, 1 when
// the engine could not be created, 2 on a protocol violation
// (oversized / truncated / invalid frame; the supervisor restarts the
// process in that case).
int shtn_host_run(std::istream& in, std::ostream& out);

#endif /* SHTN_HOST_MAIN_H */
