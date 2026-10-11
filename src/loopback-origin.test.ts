// loopback-origin.test.ts — v1.9.5 (P0): the strict loopback-origin
// parser that guards the desktop activity-transport discovery. The
// v1.9.4 startsWith prefix match accepted lookalike hosts; every refusal
// class is pinned here:
//
//   accept   — exact http://127.0.0.1:PORT and http://localhost:PORT
//   refuse   — lookalike hosts (localhost.attacker.example,
//              127.0.0.1.evil.com, suffix/precision tricks)
//   refuse   — non-http schemes (https, ws, wails, ftp)
//   refuse   — missing/out-of-range ports
//   refuse   — credentials (user:pass@), path, query, fragment
//   refuse   — non-loopback hosts, IPv6 loopback shorthand, malformed URLs

import assert from "node:assert/strict";
import { describe, it } from "node:test";

import {
  isDesktopPageOrigin,
  isValidLoopbackHttpOrigin,
} from "./loopback-origin.ts";

describe("isValidLoopbackHttpOrigin", () => {
  it("accepts the exact production loopback origins", () => {
    assert.equal(isValidLoopbackHttpOrigin("http://127.0.0.1:45911"), true);
    assert.equal(isValidLoopbackHttpOrigin("http://localhost:39281"), true);
    assert.equal(isValidLoopbackHttpOrigin("http://127.0.0.1:1"), true);
    assert.equal(isValidLoopbackHttpOrigin("http://localhost:65535"), true);
  });

  it("accepts trailing-slash normalization (URL canonical form)", () => {
    assert.equal(isValidLoopbackHttpOrigin("http://127.0.0.1:45911/"), true);
  });

  it("refuses lookalike hosts (the v1.9.4 startsWith hole)", () => {
    assert.equal(
      isValidLoopbackHttpOrigin("http://localhost.attacker.example:8080"),
      false,
    );
    assert.equal(
      isValidLoopbackHttpOrigin("http://127.0.0.1.evil.com:8080"),
      false,
    );
    assert.equal(
      isValidLoopbackHttpOrigin("http://127.0.0.1.example:8080"),
      false,
    );
    assert.equal(
      isValidLoopbackHttpOrigin("http://notwails.localhost:8080"),
      false,
    );
    assert.equal(
      isValidLoopbackHttpOrigin("http://evil.com/redirect?to=http://127.0.0.1:1"),
      false,
    );
  });

  it("refuses non-http schemes", () => {
    assert.equal(isValidLoopbackHttpOrigin("https://127.0.0.1:45911"), false);
    assert.equal(isValidLoopbackHttpOrigin("ws://127.0.0.1:45911"), false);
    assert.equal(isValidLoopbackHttpOrigin("wss://localhost:45911"), false);
    assert.equal(isValidLoopbackHttpOrigin("wails://wails.localhost"), false);
    assert.equal(isValidLoopbackHttpOrigin("ftp://127.0.0.1:45911"), false);
  });

  it("refuses missing or out-of-range ports", () => {
    assert.equal(isValidLoopbackHttpOrigin("http://127.0.0.1"), false);
    assert.equal(isValidLoopbackHttpOrigin("http://localhost"), false);
    assert.equal(isValidLoopbackHttpOrigin("http://127.0.0.1:0"), false);
    assert.equal(isValidLoopbackHttpOrigin("http://127.0.0.1:65536"), false);
    assert.equal(isValidLoopbackHttpOrigin("http://127.0.0.1:99999"), false);
    assert.equal(isValidLoopbackHttpOrigin("http://127.0.0.1:notaport"), false);
  });

  it("refuses credentials, paths, queries and fragments", () => {
    assert.equal(isValidLoopbackHttpOrigin("http://user:pass@127.0.0.1:45911"), false);
    assert.equal(isValidLoopbackHttpOrigin("http://user@127.0.0.1:45911"), false);
    assert.equal(isValidLoopbackHttpOrigin("http://127.0.0.1:45911/api"), false);
    assert.equal(isValidLoopbackHttpOrigin("http://127.0.0.1:45911/ws/activity"), false);
    assert.equal(isValidLoopbackHttpOrigin("http://127.0.0.1:45911/?token=x"), false);
    assert.equal(isValidLoopbackHttpOrigin("http://127.0.0.1:45911/#frag"), false);
  });

  it("refuses non-loopback hosts and IP lookalikes", () => {
    assert.equal(isValidLoopbackHttpOrigin("http://example.com:8080"), false);
    assert.equal(isValidLoopbackHttpOrigin("http://[::1]:45911"), false);
    assert.equal(isValidLoopbackHttpOrigin("http://0.0.0.0:45911"), false);
    assert.equal(isValidLoopbackHttpOrigin("http://127.1:45911"), false);
    assert.equal(isValidLoopbackHttpOrigin("http://[::ffff:127.0.0.1]:45911"), false);
  });

  it("refuses malformed input", () => {
    assert.equal(isValidLoopbackHttpOrigin(""), false);
    assert.equal(isValidLoopbackHttpOrigin("not a url"), false);
    assert.equal(isValidLoopbackHttpOrigin("http://"), false);
  });
});

describe("isDesktopPageOrigin", () => {
  it("recognizes the packaged desktop page origins", () => {
    assert.equal(isDesktopPageOrigin("wails:", "wails.localhost"), true);
    assert.equal(isDesktopPageOrigin("http:", "wails.localhost"), true);
  });

  it("never classifies browser/headless/dev origins as desktop", () => {
    assert.equal(isDesktopPageOrigin("http:", "127.0.0.1"), false);
    assert.equal(isDesktopPageOrigin("http:", "localhost"), false);
    assert.equal(isDesktopPageOrigin("https:", "example.com"), false);
    assert.equal(isDesktopPageOrigin("http:", "localhost.attacker.example"), false);
  });
});
