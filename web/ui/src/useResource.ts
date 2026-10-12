import { useCallback, useEffect, useRef, useState } from 'react';
import { useResourceEvents } from './events';

/**
 * useResource polls a fetcher on an interval. Everything here reflects
 * cluster state a controller changes behind the user's back, so a page that
 * fetched once would be wrong within seconds of any action. Polling pauses
 * while the tab is hidden and catches up as soon as it's visible again.
 *
 * watchKind additionally reloads the instant a matching live-update event
 * arrives (events.ts/internal/api/events.go) - e.g. 'TestCase' reloads as
 * soon as any TestCase changes, not just on the next poll tick. Polling
 * keeps running regardless, as a fallback for when the WebSocket feed is
 * reconnecting.
 */
export function useResource<T>(
  fetcher: () => Promise<T>,
  deps: unknown[] = [],
  intervalMs = 5000,
  watchKind = ''
): { data: T | undefined; error: string | undefined; reload: () => void; loading: boolean } {
  const [data, setData] = useState<T>();
  const [error, setError] = useState<string>();
  const [loading, setLoading] = useState(true);
  // Guards against a slow response for old deps landing after a newer one.
  const generation = useRef(0);

  const load = useCallback(async () => {
    const mine = ++generation.current;
    try {
      const result = await fetcher();
      if (mine === generation.current) {
        setData(result);
        setError(undefined);
      }
    } catch (err) {
      if (mine === generation.current) {
        setError(err instanceof Error ? err.message : String(err));
      }
    } finally {
      if (mine === generation.current) {
        setLoading(false);
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  useEffect(() => {
    setLoading(true);
    void load();
    if (intervalMs <= 0) {
      return;
    }
    const id = setInterval(() => {
      if (document.visibilityState === 'visible') {
        void load();
      }
    }, intervalMs);
    const onVisible = () => {
      if (document.visibilityState === 'visible') {
        void load();
      }
    };
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      clearInterval(id);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, [load, intervalMs]);

  useResourceEvents(watchKind, () => void load());

  return { data, error, loading, reload: () => void load() };
}
