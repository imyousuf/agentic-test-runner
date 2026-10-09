// The model behind the DevTools dock.
//
// The wire and the journal carry the same rows, so the live dock and the
// player dock share everything here. A request arrives as two rows joined on
// reqId, and the dock shows one line per request, because that is what a person
// is looking at.

import type { LogEvent, LogLevel, RecEvent } from './protocol';

/** Tab is which part of the dock a row belongs to. */
export type Tab = 'console' | 'network' | 'issues';

/** ConsoleRow is one line in the Console tab. */
export interface ConsoleRow {
  key: string;
  atMs: number;
  /**
   * The wall clock the tap stamped. A live page has no recording to be
   * relative to, so this is the only time it can be shown at.
   */
  ts: number;
  level: LogLevel;
  text: string;
  stack?: string;
  url?: string;
  /** How many identical lines in a row this one stands for. */
  repeat: number;
}

/** NetRow is one request, with whatever came back joined onto it. */
export interface NetRow {
  key: string;
  atMs: number;
  /** See ConsoleRow.ts. */
  ts: number;
  method: string;
  url: string;
  kind: string;
  /** Undefined while the request is still open. */
  status?: number;
  bytes?: number;
  durMs?: number;
  /** Set when the request failed, was blocked, or was cancelled. */
  error?: string;
  /** True while nothing has come back, so the dock can say so. */
  pending: boolean;
}

export interface DevToolsModel {
  console: ConsoleRow[];
  network: NetRow[];
  issues: ConsoleRow[];
  /** Rows the rate cap or the size cap refused, so a gap says it is a gap. */
  dropped: number;
  /**
   * The origin most of the requests went to, which is the page's own host in
   * every session that is not a proxy. The dock hides this one and shows every
   * other, so a call to a third party cannot pass for a call to your server.
   *
   * It cannot be read from `location`: that is the ATR server, not the page.
   */
  origin: string;
  counts: { errors: number; warnings: number; failed: number };
}

export const EMPTY_MODEL: DevToolsModel = {
  console: [],
  network: [],
  issues: [],
  dropped: 0,
  origin: '',
  counts: { errors: 0, warnings: 0, failed: 0 },
};

/**
 * build turns the rows into the three tabs.
 *
 * It runs over the whole log every time it is called, which is what makes the
 * player dock and the live dock the same code. A live dock feeds it a ring of
 * two thousand rows, and a recording of an hour is tens of thousands: both are
 * a single pass over an array, and neither is worth an incremental index.
 */
export function build(rows: LogEvent[], limit = rows.length): DevToolsModel {
  const consoleRows: ConsoleRow[] = [];
  const issues: ConsoleRow[] = [];
  const network: NetRow[] = [];
  const byReq = new Map<string, NetRow>();
  let dropped = 0;

  for (let i = 0; i < limit; i += 1) {
    const ev = rows[i];
    switch (ev.t) {
      case 'console':
      case 'error':
        pushConsole(consoleRows, textRow(ev, i, ev.t === 'error' ? 'error' : ev.level ?? 'log'));
        break;

      case 'issue':
        pushConsole(issues, textRow(ev, i, ev.level ?? 'warning'));
        break;

      case 'tap':
        pushConsole(issues, textRow(ev, i, 'info'));
        break;

      case 'drop':
        dropped += ev.count && ev.count > 1 ? ev.count : 1;
        pushConsole(issues, textRow(ev, i, 'warning'));
        break;

      case 'req': {
        const row: NetRow = {
          key: `n${i}`,
          atMs: ev.atMs,
          ts: ev.ts,
          method: ev.method ?? '',
          url: ev.url ?? '',
          kind: ev.kind ?? '',
          pending: true,
        };
        network.push(row);
        if (ev.reqId) byReq.set(ev.reqId, row);
        break;
      }

      case 'res':
      case 'netfail': {
        // A res with no req before it is normal: a recording can start in the
        // middle of a page load. It becomes a row of its own rather than
        // vanishing.
        const row = ev.reqId ? byReq.get(ev.reqId) : undefined;
        const target = row ?? orphan(network, ev, i);
        target.pending = false;
        if (ev.method && !target.method) target.method = ev.method;
        if (ev.url && !target.url) target.url = ev.url;
        if (ev.kind && !target.kind) target.kind = ev.kind;
        if (ev.status) target.status = ev.status;
        if (ev.bytes) target.bytes = ev.bytes;
        if (ev.durMs) target.durMs = ev.durMs;
        if (ev.t === 'netfail') target.error = ev.text || 'failed';
        if (ev.reqId) byReq.delete(ev.reqId);
        break;
      }
    }
  }

  return {
    console: consoleRows,
    network,
    issues,
    dropped,
    origin: mainOrigin(network),
    counts: {
      errors: consoleRows.filter((r) => r.level === 'error').length,
      warnings:
        consoleRows.filter((r) => r.level === 'warning').length +
        issues.filter((r) => r.level === 'warning').length,
      failed: network.filter(isFailed).length,
    },
  };
}

/** mainOrigin is the origin the most requests went to. */
function mainOrigin(network: NetRow[]): string {
  const seen = new Map<string, number>();
  let best = '';
  let most = 0;
  for (const row of network) {
    let origin = '';
    try {
      origin = new URL(row.url).origin;
    } catch {
      continue;
    }
    const n = (seen.get(origin) ?? 0) + 1;
    seen.set(origin, n);
    if (n > most) {
      most = n;
      best = origin;
    }
  }
  return best;
}

/** isFailed is what the dock draws in red: a failure or a 4xx and up. */
export function isFailed(row: NetRow): boolean {
  return row.error !== undefined || (row.status !== undefined && row.status >= 400);
}

function textRow(ev: LogEvent, i: number, level: LogLevel): ConsoleRow {
  return {
    key: `c${i}`,
    atMs: ev.atMs,
    ts: ev.ts,
    level,
    text: ev.text ?? '',
    stack: ev.stack,
    url: ev.url,
    repeat: ev.count && ev.count > 1 ? ev.count : 1,
  };
}

/**
 * pushConsole collapses a line that repeats straight after itself. A render
 * loop logging the same warning sixty times a second is one line with a count
 * on it, exactly as a browser console shows it.
 */
function pushConsole(out: ConsoleRow[], row: ConsoleRow): void {
  const last = out[out.length - 1];
  if (last && last.level === row.level && last.text === row.text) {
    last.repeat += row.repeat;
    return;
  }
  out.push(row);
}

function orphan(network: NetRow[], ev: LogEvent, i: number): NetRow {
  const row: NetRow = {
    key: `n${i}`,
    atMs: ev.atMs,
    ts: ev.ts,
    method: ev.method ?? '',
    url: ev.url ?? '',
    kind: ev.kind ?? '',
    pending: false,
  };
  network.push(row);
  return row;
}

/** Identical warnings closer together than this become one mark. */
const WARN_WINDOW_MS = 1000;

/**
 * warnings turns the warning lines into timeline marks.
 *
 * The recorder does not write these. A warning is not a failure, and a bar with
 * a mark on every deprecation notice is a bar nobody can read, so they are
 * derived here and shown only while the chip is on. Deciding it at record time
 * would mean a chip that changed the next recording instead of this one.
 */
export function warnings(rows: LogEvent[]): RecEvent[] {
  const out: RecEvent[] = [];
  const seen = new Map<string, { mark: RecEvent; lastMs: number }>();
  for (const ev of rows) {
    if (ev.level !== 'warning') continue;
    if (ev.t !== 'console' && ev.t !== 'issue') continue;
    const text = (ev.text ?? '').replace(/\s+/g, ' ').trim().slice(0, 200);
    const prev = seen.get(text);
    // The window slides with each repeat, so a warning that fires steadily for
    // a minute is one mark, not sixty.
    if (prev && ev.atMs - prev.lastMs <= WARN_WINDOW_MS) {
      prev.mark.count = (prev.mark.count ?? 1) + 1;
      prev.lastMs = ev.atMs;
      continue;
    }
    const mark: RecEvent = { atMs: ev.atMs, t: 'warn', reason: text, count: 1 };
    out.push(mark);
    seen.set(text, { mark, lastMs: ev.atMs });
  }
  return out;
}

/** parse reads devtools.jsonl. A truncated last line is expected after a crash. */
export function parse(text: string): LogEvent[] {
  const out: LogEvent[] = [];
  for (const line of text.split('\n')) {
    if (!line) continue;
    try {
      out.push(JSON.parse(line) as LogEvent);
    } catch {
      // A half written last line is what an interrupted recording leaves.
    }
  }
  return out;
}

/**
 * countUpTo is how many rows the playhead has reached.
 *
 * It returns a count rather than a slice on purpose. A slice is a new array on
 * every call, so the model would be rebuilt ten times a second while the video
 * plays; a count only changes when a row actually crosses the playhead, which
 * is when the model actually changes.
 */
export function countUpTo(rows: LogEvent[], atMs: number): number {
  let lo = 0;
  let hi = rows.length;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (rows[mid].atMs <= atMs) lo = mid + 1;
    else hi = mid;
  }
  return lo;
}

/** matches is the filter box: a plain substring, case insensitive. */
export function matches(needle: string, ...fields: (string | undefined)[]): boolean {
  if (!needle) return true;
  const n = needle.toLowerCase();
  return fields.some((f) => f !== undefined && f.toLowerCase().includes(n));
}
