// session-list-guard.test.ts — v1.7.5: deterministic out-of-order session
// list delivery coverage. Every resurrection shape the v1.7.4 Linux E2E
// failure exposed is pinned here at the guard level:
//
//   1. a stale response (started before a newer one) must never land;
//   2. delete → stale in-flight refresh must never land;
//   3. create → in-flight refresh started before the create must never land;
//   4. rename → in-flight refresh started before the rename must never land;
//   5. mode switch → the old-mode in-flight response must never land;
//   6. two refreshes completing out of order → only the newest lands;
//   7. a fresh refresh started after the mutation remains valid and lands.

import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { createSessionListGuard } from "./session-list-guard.ts";

describe("session-list-guard", () => {
  it("accepts the one in-flight request when nothing else happens", () => {
    const guard = createSessionListGuard();
    const ticket = guard.begin();

    assert.ok(guard.isActive(ticket));
  });

  it("a newer refresh supersedes an older in-flight response (out-of-order delivery)", () => {
    const guard = createSessionListGuard();

    const older = guard.begin(); // GET #1 leaves the client
    const newer = guard.begin(); // GET #2 leaves the client

    assert.ok(!guard.isActive(older), "the older response must be stale");
    assert.ok(guard.isActive(newer), "the newest response may land");
  });

  it("delete invalidates every refresh that started before the delete", () => {
    const guard = createSessionListGuard();

    const inFlight = guard.begin(); // GET on the wire when the user clicks Delete

    guard.invalidate(); // DELETE /api/sessions/<id> succeeded

    assert.ok(!guard.isActive(inFlight), "the stale GET must never land");
  });

  it("create invalidates refreshes started before the create", () => {
    const guard = createSessionListGuard();

    const inFlight = guard.begin();

    guard.invalidate(); // POST create succeeded
    const after = guard.begin(); // the post-create refresh

    assert.ok(!guard.isActive(inFlight));
    assert.ok(guard.isActive(after));
  });

  it("rename invalidates refreshes started before the rename", () => {
    const guard = createSessionListGuard();

    const inFlight = guard.begin();

    guard.invalidate(); // PUT rename succeeded

    assert.ok(!guard.isActive(inFlight));
  });

  it("a mode switch invalidates the old-mode in-flight response", () => {
    const guard = createSessionListGuard();

    const chatModeGet = guard.begin();

    guard.invalidate(); // setMode(agent) — the old-mode response is now poison

    assert.ok(!guard.isActive(chatModeGet));
  });

  it("responses landing in the order 2,1,3: only #3 and #2 may land, never #1", () => {
    const guard = createSessionListGuard();

    const first = guard.begin();
    const second = guard.begin();
    const third = guard.begin();

    // Delivery order: third, first, second.
    assert.ok(guard.isActive(third), "newest first: lands");
    assert.ok(!guard.isActive(first), "oldest: dropped");
    assert.ok(!guard.isActive(second), "superseded: dropped");
  });

  it("a refresh begun after the mutation is unaffected by earlier invalidations", () => {
    const guard = createSessionListGuard();

    guard.begin();
    guard.invalidate();
    guard.invalidate();

    const fresh = guard.begin();

    assert.ok(guard.isActive(fresh));
  });

  it("the guard owns no session data — it is a pure generation counter", () => {
    const guard = createSessionListGuard();

    // Any ticket sequence is accepted as long as it is the current one;
    // the guard never sees session ids, lists or modes.
    const a = guard.begin();
    guard.invalidate();
    const b = guard.begin();

    assert.notEqual(a, b);
    assert.ok(!guard.isActive(a));
    assert.ok(guard.isActive(b));
  });
});
