import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { clock, humanBytes } from './api';
import { build, isFailed, matches, type Tab } from './devtools';
import type { ConsoleRow, NetRow } from './devtools';
import type { LogEvent, LogLevel } from './protocol';

interface Props {
  rows: LogEvent[];
  /**
   * How many rows the playhead has reached. Absent in the live dock, where
   * every row that has arrived has already happened.
   */
  limit?: number;
  /**
   * Where the playhead is, so a row can say it is the one on the screen. The
   * live dock passes nothing, because "now" is the screen.
   */
  atMs?: number;
  /** Move the player to a row. Absent in the live dock, where there is nowhere to go. */
  onSeek?: (atMs: number) => void;
  onClose: () => void;
}

/** What the level filter is set to. On the Network tab "error" reads as failed. */
type Level = 'all' | 'warning' | 'error';

const MIN_WIDTH = 320;
const DEFAULT_WIDTH = 520;
const WIDTH_KEY = 'atr.dock.width';

/**
 * DevTools is the dock beside the picture.
 *
 * It is deliberately not a browser DevTools. It shows what was kept, and what
 * was kept is metadata: no request body, no response body, no header. So the
 * three tabs are the three questions somebody actually asks of a recording —
 * what did the page say, what did it ask for, and what went wrong.
 *
 * One row is one line. A log is read by scanning down a column, and a row that
 * wraps breaks the column, so everything that does not fit goes into the row
 * you open rather than onto a second line of every row you do not.
 */
export function DevTools({ rows, limit, atMs, onSeek, onClose }: Props) {
  const [tab, setTab] = useState<Tab>('console');
  const [filter, setFilter] = useState('');
  const [level, setLevel] = useState<Level>('all');
  const [open, setOpen] = useState('');
  const [width, setWidth] = useState(readWidth);

  const model = useMemo(() => build(rows, limit), [rows, limit]);

  // No playhead means a live page. There is no recording for a row to be
  // relative to, so the rows are stamped with the time of day instead.
  const live = atMs === undefined;

  const consoleRows = model.console.filter(
    (r) => keepLevel(r.level, level) && matches(filter, r.text, r.url),
  );
  const netRows = model.network.filter(
    (r) => (level !== 'error' || isFailed(r)) && matches(filter, r.url, r.method, r.kind),
  );
  const issueRows = model.issues.filter(
    (r) => keepLevel(r.level, level) && matches(filter, r.text, r.url),
  );

  const counts: Record<Tab, number> = {
    console: model.console.length,
    network: model.network.length,
    issues: model.issues.length,
  };

  const shown =
    tab === 'console' ? consoleRows.length : tab === 'network' ? netRows.length : issueRows.length;
  const problems = tab === 'network' ? model.counts.failed : model.counts.errors;
  const total = counts[tab];
  /** Nothing to show because of the filter is a different answer from nothing to show. */
  const hidden = total > 0 && shown === 0;

  /**
   * The dock follows the playhead, and a live dock follows the page. Only the
   * rows up to now are in the list, so the newest one is the last one, and
   * following it means staying at the bottom.
   *
   * It stops following the moment somebody scrolls up, because at that point
   * they are reading, and a list that yanks itself back down is a list nobody
   * can read.
   */
  const body = useRef<HTMLDivElement>(null);
  const stick = useRef(true);

  useEffect(() => {
    const el = body.current;
    if (!el) return;
    const onScroll = () => {
      stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    };
    el.addEventListener('scroll', onScroll, { passive: true });
    return () => el.removeEventListener('scroll', onScroll);
  }, []);

  // Before the paint, so the list never shows the wrong place first.
  useLayoutEffect(() => {
    const el = body.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [shown, tab]);

  useEffect(() => {
    try {
      localStorage.setItem(WIDTH_KEY, String(width));
    } catch {
      // A browser with storage turned off still gets to drag the handle.
    }
  }, [width]);

  /** The handle drags the left edge, so the dock grows as the pointer moves left. */
  const startResize = (ev: React.PointerEvent) => {
    ev.preventDefault();
    const fromX = ev.clientX;
    const fromW = width;
    const move = (e: PointerEvent) =>
      setWidth(Math.max(MIN_WIDTH, Math.min(window.innerWidth - 240, fromW + fromX - e.clientX)));
    const stop = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', stop);
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', stop);
  };

  const pick = (row: { key: string; atMs: number }) => {
    setOpen((k) => (k === row.key ? '' : row.key));
    onSeek?.(row.atMs);
  };

  return (
    <aside className="dock" style={{ width }}>
      <div
        className="dock-grip"
        onPointerDown={startResize}
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize the dock"
      />

      <div className="dock-head">
        <span className="seg">
          {(['console', 'network', 'issues'] as Tab[]).map((name) => (
            <button
              key={name}
              type="button"
              className={tab === name ? 'on' : ''}
              onClick={() => setTab(name)}
            >
              {name[0].toUpperCase() + name.slice(1)}
              {counts[name] > 0 && <span className="pill">{counts[name]}</span>}
            </button>
          ))}
        </span>
        <button type="button" className="btn btn-icon" title="Close" onClick={onClose}>
          ✕
        </button>
      </div>

      <div className="dock-bar">
        <input
          type="search"
          placeholder="Filter"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
        {/* The counts live on the buttons that act on them, so "how bad is it"
            and "show me" are one control. The Network tab has no warnings to
            offer, so it is not offered one. */}
        <span className="seg seg-sm">
          <button
            type="button"
            className={level === 'all' ? 'on' : ''}
            onClick={() => setLevel('all')}
          >
            All
          </button>
          {tab !== 'network' && (
            <button
              type="button"
              className={level === 'warning' ? 'on' : ''}
              onClick={() => setLevel('warning')}
            >
              Warnings
              {model.counts.warnings > 0 && <span className="pill">{model.counts.warnings}</span>}
            </button>
          )}
          <button
            type="button"
            className={level === 'error' ? 'on' : ''}
            onClick={() => setLevel('error')}
          >
            {tab === 'network' ? 'Failed' : 'Errors'}
            {problems > 0 && <span className="pill bad">{problems}</span>}
          </button>
        </span>
      </div>

      <div className="dock-body" ref={body}>
        {tab === 'console' && (
          <TextList
            rows={consoleRows}
            atMs={atMs}
            live={live}
            open={open}
            onPick={pick}
            empty={hidden ? null : 'The page said nothing.'}
          />
        )}
        {tab === 'network' && (
          <NetList
            rows={netRows}
            atMs={atMs}
            live={live}
            open={open}
            onPick={pick}
            origin={model.origin}
            empty={hidden ? null : 'The page asked for nothing.'}
          />
        )}
        {tab === 'issues' && (
          <TextList
            rows={issueRows}
            atMs={atMs}
            live={live}
            open={open}
            onPick={pick}
            empty={hidden ? null : 'No CSP, CORS or deprecation notice.'}
          />
        )}

        {hidden && (
          <p className="dim small pad">
            All {total} hidden by the filter.{' '}
            <button
              type="button"
              className="link"
              onClick={() => {
                setFilter('');
                setLevel('all');
              }}
            >
              Show them
            </button>
          </p>
        )}
      </div>

      {/* A gap in the log always says it is a gap. */}
      {model.dropped > 0 && (
        <div className="dock-foot dim small">
          {model.dropped} lines were dropped, because the page reported faster than the log
          keeps.
        </div>
      )}
    </aside>
  );
}

/** keepLevel is the level filter. "Warnings" means warnings and worse. */
function keepLevel(rowLevel: LogLevel, want: Level): boolean {
  if (want === 'all') return true;
  if (want === 'error') return rowLevel === 'error';
  return rowLevel === 'error' || rowLevel === 'warning';
}

interface ListProps<T> {
  rows: T[];
  atMs?: number;
  live: boolean;
  open: string;
  onPick: (row: { key: string; atMs: number }) => void;
  /** Null when the list is empty only because of the filter, which says so itself. */
  empty: string | null;
}

function TextList({ rows, atMs, live, open, onPick, empty }: ListProps<ConsoleRow>) {
  if (rows.length === 0) return empty ? <p className="dim small pad">{empty}</p> : null;
  return (
    <ul className="log">
      {rows.map((r) => {
        const shown = open === r.key;
        return (
          <li key={r.key} className={`log-row ${r.level}${here(r.atMs, atMs) ? ' now' : ''}`}>
            <button type="button" className="log-line" onClick={() => onPick(r)}>
              <span className="log-at num">{stamp(r, live)}</span>
              <span className="log-lvl num">{TAG[r.level]}</span>
              <span className="log-text">{r.text}</span>
              {r.repeat > 1 && <span className="pill">{r.repeat}</span>}
            </button>
            {shown && (
              <div className="log-open">
                <p className="log-full">{r.text}</p>
                {r.stack && <pre className="log-stack">{r.stack}</pre>}
                {r.url && <p className="log-kv">{r.url}</p>}
              </div>
            )}
          </li>
        );
      })}
    </ul>
  );
}

function NetList({
  rows,
  atMs,
  live,
  open,
  onPick,
  origin,
  empty,
}: ListProps<NetRow> & { origin: string }) {
  if (rows.length === 0) return empty ? <p className="dim small pad">{empty}</p> : null;
  return (
    <ul className="log">
      {rows.map((r) => {
        const shown = open === r.key;
        const bad = isFailed(r);
        return (
          <li key={r.key} className={`log-row${bad ? ' error' : ''}${here(r.atMs, atMs) ? ' now' : ''}`}>
            <button type="button" className="log-line" onClick={() => onPick(r)}>
              <span className="log-at num">{stamp(r, live)}</span>
              <span className={`net-status num ${statusClass(r)}`}>
                {r.error ? 'ERR' : r.pending ? '···' : (r.status ?? '')}
              </span>
              <span className="net-method num">{r.method}</span>
              <span className="log-text">{short(r.url, origin)}</span>
              <span className="net-meta num">{r.error ? '' : joinMeta(r)}</span>
            </button>
            {shown && (
              <div className="log-open">
                <p className="log-full">{r.url}</p>
                {r.error && <p className="log-bad">{r.error}</p>}
                <p className="log-kv">
                  {[
                    r.kind,
                    r.pending ? 'still open' : undefined,
                    r.status ? `status ${r.status}` : undefined,
                    r.durMs !== undefined ? `${r.durMs} ms` : undefined,
                    r.bytes !== undefined ? humanBytes(r.bytes) : undefined,
                  ]
                    .filter(Boolean)
                    .join(' · ')}
                </p>
              </div>
            )}
          </li>
        );
      })}
    </ul>
  );
}

/**
 * The level tag. Colour alone cannot carry this: the dock is read at a glance
 * over a page of any colour, and a red line and an amber line are the same line
 * to plenty of people.
 */
const TAG: Record<LogLevel, string> = {
  error: 'ERR',
  warning: 'WRN',
  info: '',
  log: '',
  debug: 'DBG',
};

function statusClass(r: NetRow): string {
  if (r.error) return 'bad';
  if (r.pending) return 'open';
  if (r.status !== undefined && r.status >= 400) return 'bad';
  return '';
}

/**
 * stamp writes the time column. In a recording it is the offset from the
 * start, because that is what the scrub bar shows. On a live page it is the
 * time of day, because there is no start to count from.
 */
function stamp(row: { atMs: number; ts: number }, live: boolean): string {
  if (!live) return clock(row.atMs);
  return new Date(row.ts).toLocaleTimeString(undefined, { hour12: false });
}

/** A row within a second of the playhead is the one on the screen. */
function here(rowMs: number, atMs?: number): boolean {
  return atMs !== undefined && Math.abs(rowMs - atMs) < 1000;
}

function joinMeta(r: NetRow): string {
  const parts: string[] = [];
  if (r.durMs !== undefined) parts.push(`${r.durMs} ms`);
  if (r.bytes !== undefined) parts.push(humanBytes(r.bytes));
  return parts.join(' · ');
}

/**
 * short drops the origin of a request to the page's own host, because every
 * row of one session shares it and the path is what tells them apart. It keeps
 * the host of a request to anybody else: "which third party was that" is a
 * question the path cannot answer, and a stripped host makes a call to an
 * analytics vendor look like a call to your own server.
 */
function short(url: string, origin: string): string {
  try {
    const u = new URL(url);
    if (u.origin === origin) return u.pathname + u.search;
    return u.host + u.pathname + u.search;
  } catch {
    return url;
  }
}

function readWidth(): number {
  try {
    const saved = Number(localStorage.getItem(WIDTH_KEY));
    if (saved >= MIN_WIDTH) return saved;
  } catch {
    // No storage, so the default it is.
  }
  return DEFAULT_WIDTH;
}
