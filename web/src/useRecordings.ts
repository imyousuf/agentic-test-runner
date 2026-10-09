import { useCallback, useEffect, useState } from 'react';
import { api } from './api';
import type { RecordingSummary } from './protocol';

/** useRecordings loads the library and keeps it in step with the server. */
export function useRecordings(active: boolean) {
  const [items, setItems] = useState<RecordingSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const reload = useCallback(async () => {
    try {
      setItems(await api.listRecordings());
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!active) return;
    void reload();
  }, [active, reload]);

  return { items, loading, error, reload, setError };
}
