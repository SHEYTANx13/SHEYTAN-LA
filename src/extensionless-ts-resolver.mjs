// extensionless-ts-resolver.mjs — a Node module-resolution hook for the
// v1.8.3 store-level regression tests.
//
// The application's source uses Vite/TS extension-less relative imports
// (`import ... from "./api"`), which the bundler resolves but plain Node
// ESM does not. The repository's `node --test` units therefore only ever
// imported PURE leaf modules — which made the store itself (the exact
// production state machine the run-107 repair touches) unreachable from
// deterministic unit tests.
//
// This hook adds the missing `.ts` extension for relative specifiers
// ONLY when the plain resolution fails first (packages and explicit
// extensions always take the default path). It is registered from
// src/session-delete-regression.test.ts and therefore affects that one
// test process only — every other test keeps stock Node resolution.

export async function resolve(specifier, context, nextResolve) {
  try {
    return await nextResolve(specifier, context);
  } catch (error) {
    const isRelative =
      specifier.startsWith("./") ||
      specifier.startsWith("../") ||
      specifier.startsWith("/");

    if (isRelative && !specifier.endsWith(".ts")) {
      try {
        return await nextResolve(`${specifier}.ts`, context);
      } catch {
        // fall through to the original failure
      }
    }

    throw error;
  }
}
