/// <reference types="vite/client" />

// v1.3.6 (spec §29/§30): the build-time canonical frontend version,
// injected by vite.config.ts `define` from package.json — the single
// release version source. The backend's runtime appVersion (/api/state)
// remains authoritative once loaded; this constant guarantees the FIRST
// paint already shows the correct version instead of a stale literal.
declare const __APP_VERSION__: string;
