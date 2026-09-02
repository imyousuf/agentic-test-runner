import type { Manifest, RecordingSummary } from './protocol';

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init);
  const text = await res.text();
  let body: unknown = null;
  try {
    body = text ? JSON.parse(text) : null;
  } catch {
    body = null;
  }
  if (!res.ok) {
    const message =
      body && typeof body === 'object' && 'error' in body
        ? String((body as { error: unknown }).error)
        : `${res.status} ${res.statusText}`;
    throw new Error(message);
  }
  return body as T;
}

export const api = {
  startRecording: (title: string) =>
    request<{ id: string }>('/api/record/start', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title }),
    }),

  stopRecording: () =>
    request<{ id: string; frames: number; durationMs: number; bytes: number }>(
      '/api/record/stop',
      { method: 'POST' },
    ),

  listRecordings: () =>
    request<{ recordings: RecordingSummary[] }>('/api/recordings').then(
      (r) => r.recordings ?? [],
    ),

  manifest: (id: string) => request<Manifest>(`/api/recordings/${id}/manifest.json`),

  rename: (id: string, title: string) =>
    request<{ status: string }>(`/api/recordings/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title }),
    }),

  remove: (id: string) =>
    request<{ status: string }>(`/api/recordings/${id}`, { method: 'DELETE' }),

  encode: (id: string) =>
    request<{ url: string }>(`/api/recordings/${id}/encode`, { method: 'POST' }),

  repair: (id: string) =>
    request<{ id: string; frames: number }>(`/api/recordings/${id}/repair`, {
      method: 'POST',
    }),

  /**
   * devtools reads the whole log journal as text. It is not JSON: it is one
   * JSON object a line, so a crashed recording still parses up to its last
   * complete line. An empty string means the recording has no journal.
   */
  devtools: (id: string) =>
    fetch(`/api/recordings/${id}/devtools.jsonl`).then((r) =>
      r.ok ? r.text() : '',
    ),

  frameURL: (id: string, file: string) => `/api/recordings/${id}/frames/${file}`,

  mp4URL: (id: string) => `/api/recordings/${id}/recording.mp4`,

  /** A plain href, so the browser downloads it rather than buffering it here. */
  exportURL: (id: string, withMP4 = false) =>
    `/api/recordings/${id}/export.zip` + (withMP4 ? '?mp4=1' : ''),

  /**
   * The zip goes up as the raw body. There is one file and no other field, so
   * multipart would only add a boundary to parse on both sides.
   */
  importZip: async (file: File, force = false) => {
    const res = await fetch('/api/recordings/import' + (force ? '?force=1' : ''), {
      method: 'POST',
      body: file,
    });
    const body = (await res.json()) as { id?: string; error?: string; skipped?: number };
    if (!res.ok) throw new Error(body.error ?? `import failed (${res.status})`);
    return body;
  },
};

/** humanBytes formats a byte count the way the CLI does. */
export function humanBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let value = n / 1024;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i += 1;
  }
  return `${value.toFixed(1)} ${units[i]}`;
}

/** clock formats milliseconds as m:ss, or h:mm:ss past an hour. */
export function clock(ms: number): string {
  const total = Math.max(0, Math.round(ms / 1000));
  const s = total % 60;
  const m = Math.floor(total / 60) % 60;
  const h = Math.floor(total / 3600);
  const pad = (n: number) => String(n).padStart(2, '0');
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`;
}
