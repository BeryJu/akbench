/*
 * Copyright 2026
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

import { useCallback, useEffect, useRef, useState } from 'react';

/**
 * useResource polls a fetcher on an interval. Everything here reflects
 * cluster state a controller changes behind the user's back, so a page that
 * fetched once would be wrong within seconds of any action. Polling pauses
 * while the tab is hidden and catches up as soon as it's visible again.
 */
export function useResource<T>(
  fetcher: () => Promise<T>,
  deps: unknown[] = [],
  intervalMs = 5000
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

  return { data, error, loading, reload: () => void load() };
}
