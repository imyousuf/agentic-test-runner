import type { Manifest, RecordingSummary } from './protocol';

/**
 * token comes from the URL the CLI printed. The server also sets a cookie on
 * that first request, so most calls would work without it; the query parameter
 * keeps working if the cookie is ever refused.
 */
export const token = new URLSearchParams(location.search).get('t') ?? '';

/** withToken appends the token to a same-origin path. */
export function withToken(path: string): string {
  if (!token) return path;
  return path + (path.includes('?') ? '&' : '?') + 't=' + encodeURIComponent(token);
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(withToken(path), init);
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

  frameURL: (id: string, file: string) => withToken(`/api/recordings/${id}/frames/${file}`),

  mp4URL: (id: string) => withToken(`/api/recordings/${id}/recording.mp4`),
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
