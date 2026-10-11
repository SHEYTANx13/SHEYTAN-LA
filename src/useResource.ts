// useResource.ts — v1.2.6 React binding for the shared resource layer.
//
// useResource subscribes a component to ONE cached resource (dedup + TTL +
// stale-while-revalidate + last-known-good) without owning a second copy of
// the data. `enabled` gates the first fetch — lazy resources (models list,
// deep diagnostics) start loading only when the surface that needs them is
// actually shown.
import { useCallback, useEffect, useRef, useState } from "react";

import {
  ensure,
  get as getResource,
  subscribe,
  type ResourceSnapshot,
} from "./resources";

export type ResourceHook<T> = {
  state: ResourceSnapshot<T>["state"];
  data: T | null;
  error: string | null;
  fetchedAt: number | null;
  refresh: () => void;
};

export function useResource<T>(
  key: string,
  fetcher: (signal: AbortSignal) => Promise<T>,
  options?: { enabled?: boolean },
): ResourceHook<T> {
  const enabled = options?.enabled !== false;

  const [snap, setSnap] = useState<ResourceSnapshot<T>>(() =>
    getResource<T>(key),
  );

  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;

  useEffect(() => {
    if (!enabled) {
      return;
    }

    const unsubscribe = subscribe<T>(key, setSnap);

    void ensure<T>(key, (signal) => fetcherRef.current(signal)).catch(() => {
      // ensure() resolves with null on failure when no last-known-good
      // exists; the error is carried in the snapshot state.
    });

    return unsubscribe;
  }, [key, enabled]);

  const refresh = useCallback(() => {
    void ensure<T>(key, (signal) => fetcherRef.current(signal), {
      force: true,
    }).catch(() => {
      // Error state lands in the snapshot.
    });
  }, [key]);

  return {
    state: snap.state,
    data: snap.data,
    error: snap.error,
    fetchedAt: snap.fetchedAt,
    refresh,
  };
}
