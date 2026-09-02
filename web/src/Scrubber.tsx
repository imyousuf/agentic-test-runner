import { useMemo, useRef } from 'react';
import { clock } from './api';
import { KIND_LABEL, type Activity, type Mark } from './activity';
import type { FrameRecord } from './protocol';

/** How many columns the activity bar is drawn with. */
const BUCKETS = 240;
/** A score below this reads as nothing at all, so the bars start here. */
const FLOOR = 0.0002;
/** A score at or above this is a full height bar, such as a page navigation. */
const CEILING = 0.1;
/** Narrower than this share of the bar and a duration label will not fit. */
const LABEL_MIN_PCT = 3;

interface Props {
  frames: FrameRecord[];
  marks: Mark[];
  act: Activity;
  /** Played position of each frame, from the activity timeline. */
  playAt: number[];
  total: number;
  pos: number;
  collapsed: (span: number) => boolean;
  onSeek: (t: number) => void;
  onExpand: (span: number) => void;
}

/**
 * Scrubber is the activity timeline.
 *
 * A plain progress bar of a session recording says nothing: most of a session
 * is a still page, and the interesting seconds look exactly like the boring
 * ones. This draws the change score instead, so the shape of the bar is the
 * shape of the session, and it marks the stretches where nothing happened so
 * they can be skipped or opened.
 *
 * The row along the top says what happened, and the bars below say how much
 * moved. The two answer different questions: the bars find the busy part of a
 * session, the marks find the click that started it.
 */
export function Scrubber({
  frames,
  marks,
  act,
  playAt,
  total,
  pos,
  collapsed,
  onSeek,
  onExpand,
}: Props) {
  const track = useRef<HTMLDivElement>(null);

  // The bars are a function of the recording and of which spans are open, so
  // they are rebuilt on a seek only if one of those changed.
  const bars = useMemo(() => {
    const out = new Array<number>(BUCKETS).fill(0);
    if (total <= 0) return out;
    for (let i = 0; i < frames.length; i += 1) {
      const b = Math.min(BUCKETS - 1, Math.floor((playAt[i] / total) * BUCKETS));
      const s = act.scores[i] ?? 0;
      if (s > out[b]) out[b] = s;
    }
    return out.map(height);
  }, [frames, playAt, total, act]);

  const at = (t: number) => (total > 0 ? (t / total) * 100 : 0);

  const seekFromEvent = (clientX: number) => {
    const box = track.current?.getBoundingClientRect();
    if (!box || box.width === 0) return;
    onSeek(((clientX - box.left) / box.width) * total);
  };

  return (
    <div
      className="scrub"
      ref={track}
      role="slider"
      tabIndex={0}
      aria-label="Timeline"
      aria-valuemin={0}
      aria-valuemax={Math.round(total)}
      aria-valuenow={Math.round(pos)}
      aria-valuetext={clock(pos)}
      onPointerDown={(e) => seekFromEvent(e.clientX)}
      onKeyDown={(e) => {
        if (e.key === 'Home') onSeek(0);
        if (e.key === 'End') onSeek(total);
      }}
    >
      <div className="scrub-marks">
        {marks.map((m, i) => (
          <button
            type="button"
            key={i}
            className={`mark ${m.kind}`}
            style={{ left: `${at(m.at)}%` }}
            title={describe(m)}
            aria-label={describe(m)}
            onPointerDown={(e) => e.stopPropagation()}
            onClick={(e) => {
              e.stopPropagation();
              onSeek(m.at);
            }}
          >
            <span className="mark-pip" />
          </button>
        ))}
      </div>

      <div className="scrub-bars">
        {bars.map((h, i) => (
          <span key={i} style={{ height: `${h * 100}%` }} />
        ))}
      </div>

      {act.spans.map((sp, i) => {
        if (!sp.idle) return null;
        const cut = collapsed(i);
        const width = Math.max(at(playAt[sp.to] - playAt[sp.from]), 0.4);
        return (
          <button
            type="button"
            key={i}
            className={`scrub-idle${cut ? ' cut' : ''}`}
            style={{ left: `${at(playAt[sp.from])}%`, width: `${width}%` }}
            title={
              cut
                ? `${clock(sp.realMs)} of inactivity was cut. Click to play it.`
                : `${clock(sp.realMs)} of inactivity`
            }
            onPointerDown={(e) => e.stopPropagation()}
            onClick={(e) => {
              e.stopPropagation();
              if (cut) onExpand(i);
            }}
          >
            {/* A clipped label reads as a wrong number, so it is all or nothing. */}
            {width >= LABEL_MIN_PCT && <span className="scrub-idle-label">{clock(sp.realMs)}</span>}
          </button>
        );
      })}

      <div className="scrub-played" style={{ width: `${at(pos)}%` }} />
      <div className="scrub-head" style={{ left: `${at(pos)}%` }} />
    </div>
  );
}

/** describe writes the tooltip of one mark, or of a cluster of them. */
function describe(m: Mark): string {
  const one = (ev: Mark['events'][number]) => {
    const what = KIND_LABEL[ev.t] ?? ev.t;
    const detail = ev.url ?? ev.reason ?? '';
    // A repeat says "×50" rather than putting fifty marks in one place.
    const times = ev.count && ev.count > 1 ? ` ×${ev.count}` : '';
    return (detail ? `${what} ${detail}` : what) + times;
  };
  const head = `${clock(m.events[0].atMs)} · ${one(m.events[0])}`;
  if (m.events.length === 1) return head;
  return `${head} (+${m.events.length - 1} more)`;
}

/**
 * height maps a score to a bar height on a log scale. The scores span three
 * decades — a caret is 0.0002 and a page navigation is near 1 — so a linear
 * bar would show a flat line with a few spikes and hide everything between.
 */
function height(score: number): number {
  if (score <= FLOOR) return 0;
  const h = Math.log(score / FLOOR) / Math.log(CEILING / FLOOR);
  return Math.max(0.06, Math.min(1, h));
}
