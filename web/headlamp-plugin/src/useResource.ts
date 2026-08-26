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

// Ported unchanged in behavior from web/frontend/components/ui.tsx.
import { useCallback, useEffect, useState } from 'react';
import { ApiError } from './api';

/**
 * useResource polls a fetcher on an interval. Everything here reflects
 * cluster state a controller changes behind the user's back, so a page that
 * fetched once would be wrong within seconds of any action.
 */
export function useResource<T>(
  fetcher: () => Promise<T>,
  deps: unknown[] = [],
  intervalMs = 5000
): { data: T | undefined; error: string | undefined; reload: () => void; loading: boolean } {
  const [data, setData] = useState<T>();
  const [error, setError] = useState<string>();
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    try {
      setData(await fetcher());
      setError(undefined);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err));
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  useEffect(() => {
    void load();
    if (intervalMs <= 0) {
      return;
    }
    const id = setInterval(() => void load(), intervalMs);
    return () => clearInterval(id);
  }, [load, intervalMs]);

  return { data, error, loading, reload: () => void load() };
}
