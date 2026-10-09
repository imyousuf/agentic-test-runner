import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { analyse, frameAt, IDLE_SHOWN_MS, marks, timeline } from './activity';
import { api, clock, humanBytes } from './api';
import { DevTools } from './DevTools';
import { countUpTo, parse, warnings } from './devtools';
import { FrameCanvas } from './FrameCanvas';
import { Icon } from './Icon';
import { Scrubber } from './Scrubber';
import type { LogEvent, Manifest } from './protocol';

/** How far ahead to decode. About six seconds at ten frames a second. */
const PREFETCH = 60;
/** How far behind to keep, so a small step back does not refetch. */
const KEEP_BEHIND = 12;

const SPEEDS = [0.5, 1, 2, 4];

interface Props {
  id: string;
  onBack: () => void;
}

/**
 * Player draws a recording on a canvas.
 *
 * A recording is JPEG frames and a manifest, so playing one needs no video
 * element and no codec. That is also why the player owns the clock: the frames
 * arrive at whatever rate the page produced them, and the manifest says when
 * each one belongs.
 */
export function Player({ id, onBack }: Props) {
  const [manifest, setManifest] = useState<Manifest | null>(null);
  const [error, setError] = useState('');
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState(1);
  // A recording is mostly waiting, so the useful default is to cut the waiting.
  const [skipIdle, setSkipIdle] = useState(true);
  // Idle spans the viewer opened by hand. Nothing was thrown away, so any of
  // them can be played in full.
  const [opened, setOpened] = useState<Set<number>>(new Set());
  const [pos, setPos] = useState(0);
  const [encoding, setEncoding] = useState(false);
  const [mp4, setMp4] = useState(false);
  const [log, setLog] = useState<LogEvent[]>([]);
  const [dock, setDock] = useState(false);
  const [showWarnings, setShowWarnings] = useState(false);

  const posRef = useRef(0);
  const drawnRef = useRef(-1);
  const cache = useRef(new Map<number, ImageBitmap>());
  const inFlight = useRef(new Set<number>());
  // gen rises whenever the cache is thrown away, so a decode that was already
  // in flight cannot put an old frame into the new recording's cache.
  const gen = useRef(0);

  useEffect(() => {
    let live = true;
    setManifest(null);
    setError('');
    api
      .manifest(id)
      .then((m) => live && setManifest(m))
      .catch((err) => live && setError(err instanceof Error ? err.message : String(err)));
    fetch(api.mp4URL(id), { method: 'HEAD' })
      .then((r) => live && setMp4(r.ok))
      .catch(() => undefined);
    // The whole journal at once. It is text, it is capped at twenty megabytes,
    // and every part of the dock needs a different slice of it, so streaming it
    // would only move the parsing to a worse place.
    setLog([]);
    api
      .devtools(id)
      .then((text) => live && setLog(parse(text)))
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [id]);

  const frames = manifest?.frames ?? [];

  const act = useMemo(() => analyse(manifest), [manifest]);

  const collapsed = useCallback(
    (span: number) => skipIdle && !opened.has(span),
    [skipIdle, opened],
  );

  /**
   * playAt maps each frame to its position on the playback timeline. With
   * "skip inactivity" on, a quiet stretch is squeezed, so the timeline is no
   * longer the recording clock and every lookup has to go through this array.
   */
  const playAt = useMemo(
    () => timeline(frames, act, collapsed),
    [frames, act, collapsed],
  );

  const total = playAt.length > 0 ? playAt[playAt.length - 1] + IDLE_SHOWN_MS : 0;

  /** What happened, placed on the playback clock. */
  const warnMarks = useMemo(() => (showWarnings ? warnings(log) : []), [showWarnings, log]);
  const events = useMemo(
    () => [...(manifest?.events ?? []), ...warnMarks],
    [manifest, warnMarks],
  );
  const acts = useMemo(() => marks(frames, events, playAt), [frames, events, playAt]);

  /** How much real time the current cuts remove. */
  const cutMs = act.spans.reduce(
    (n, sp, i) => (sp.idle && collapsed(i) ? n + Math.max(0, sp.realMs - IDLE_SHOWN_MS) : n),
    0,
  );

  /** indexAt finds the last frame whose turn has come. */
  const indexAt = useCallback(
    (t: number) => {
      let lo = 0;
      let hi = playAt.length - 1;
      let best = 0;
      while (lo <= hi) {
        const mid = (lo + hi) >> 1;
        if (playAt[mid] <= t) {
          best = mid;
          lo = mid + 1;
        } else {
          hi = mid - 1;
        }
      }
      return best;
    },
    [playAt],
  );

  /**
   * gapScale is how much playback time one millisecond of recording time buys
   * in the stretch that runs up to frame i. It is 1 everywhere except in a
   * quiet stretch that has been cut, where the whole stretch is squeezed into
   * IDLE_SHOWN_MS.
   *
   * The index is the later frame of the pair, because that is how timeline()
   * accumulates: the gap before frame i is scaled by the span frame i is in.
   */
  const gapScale = useCallback(
    (i: number) => {
      if (i <= 0 || i >= frames.length) return 1;
      const at = act.spanOf[i];
      const sp = act.spans[at];
      if (!sp || !sp.idle || !collapsed(at) || sp.realMs <= 0) return 1;
      return IDLE_SHOWN_MS / sp.realMs;
    },
    [frames, act, collapsed],
  );

  /**
   * The playhead in recording time. The dock rows are stamped on that clock,
   * and the playback clock is a different one as soon as one quiet stretch is
   * cut, so the two have to be converted rather than compared.
   *
   * It interpolates inside the gap rather than snapping to the frame. This
   * recording holds one frame for seven seconds, and a clock that only moved
   * when the picture did would leave the dock seven seconds behind the bar.
   *
   * Neither direction rounds. Inside a cut stretch the scale can be 1:90, so
   * half a millisecond of playback is most of a second of recording, and
   * rounding here would seek to a log row and then hide it.
   */
  const realAt = useCallback(
    (t: number) => {
      if (frames.length === 0) return 0;
      const i = indexAt(t);
      return frames[i].atMs + (t - playAt[i]) / gapScale(i + 1);
    },
    [frames, indexAt, playAt, gapScale],
  );

  /** playPos is realAt backwards: where a moment of the recording is played. */
  const playPos = useCallback(
    (atMs: number) => {
      if (frames.length === 0) return 0;
      const i = frameAt(frames, atMs);
      return playAt[i] + (atMs - frames[i].atMs) * gapScale(i + 1);
    },
    [frames, playAt, gapScale],
  );

  const realMs = realAt(pos);
  /**
   * How much of the log the playhead has reached. A dock that showed the whole
   * journal would answer a question about the end of the session while the
   * picture is still at the start of it.
   *
   * A count, not a slice: the playhead moves ten times a second and the visible
   * set changes far less often than that.
   *
   * The extra millisecond is the rounding in realAt and playPos. Without it,
   * clicking a row seeks to that row and then hides it, which reads as the row
   * having been deleted by the click.
   */
  const seen = useMemo(() => countUpTo(log, realMs + 1), [log, realMs]);

  // Decode a window around the playhead, and close what falls outside it.
  // ImageBitmap holds decoded pixels, so an unbounded cache of a long
  // recording would use hundreds of megabytes.
  const prefetch = useCallback(
    (from: number) => {
      const map = cache.current;
      for (const [key, bitmap] of map) {
        if (key < from - KEEP_BEHIND || key > from + PREFETCH) {
          bitmap.close();
          map.delete(key);
        }
      }
      const mine = gen.current;
      for (let i = from; i < Math.min(frames.length, from + PREFETCH); i += 1) {
        if (map.has(i) || inFlight.current.has(i)) continue;
        inFlight.current.add(i);
        const want = i;
        fetch(api.frameURL(id, frames[want].file))
          .then((r) => r.blob())
          .then(createImageBitmap)
          .then((bitmap) => {
            inFlight.current.delete(want);
            if (gen.current === mine) map.set(want, bitmap);
            else bitmap.close();
          })
          .catch(() => inFlight.current.delete(want));
      }
    },
    [frames, id],
  );

  // Throw the whole cache away when the recording changes.
  useEffect(() => {
    return () => {
      gen.current += 1;
      for (const bitmap of cache.current.values()) bitmap.close();
      cache.current.clear();
      inFlight.current.clear();
      drawnRef.current = -1;
      posRef.current = 0;
    };
  }, [id]);

  useEffect(() => {
    if (frames.length > 0) prefetch(0);
  }, [frames, prefetch]);

  // The playback clock. It advances posRef every animation frame and pushes
  // the value into React ten times a second, which is enough for the scrub bar
  // and far cheaper than a render per frame.
  useEffect(() => {
    if (!playing || total === 0) return;
    let running = true;
    let last = performance.now();
    let lastUI = 0;

    const tick = (now: number) => {
      if (!running) return;
      posRef.current += (now - last) * speed;
      last = now;
      if (posRef.current >= total) {
        posRef.current = total;
        setPos(total);
        setPlaying(false);
        return;
      }
      if (now - lastUI > 100) {
        lastUI = now;
        setPos(posRef.current);
      }
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
    return () => {
      running = false;
    };
  }, [playing, speed, total]);

  const nextBitmap = useCallback(() => {
    if (frames.length === 0) return null;
    const i = indexAt(posRef.current);
    if (i !== drawnRef.current) {
      const bitmap = cache.current.get(i);
      if (bitmap) {
        drawnRef.current = i;
        prefetch(i);
        return bitmap;
      }
      // Not decoded yet. Keep the current image rather than blanking.
      prefetch(i);
    }
    return null;
  }, [frames, indexAt, prefetch]);

  // The canvas owns nothing: the cache closes every bitmap, so onDrawn must
  // not.
  const keepBitmap = useCallback(() => undefined, []);

  const seek = (t: number) => {
    posRef.current = Math.max(0, Math.min(total, t));
    setPos(posRef.current);
    drawnRef.current = -1;
    prefetch(indexAt(posRef.current));
  };

  /**
   * seekReal moves to a moment in recording time. A dock row knows when it
   * happened, not where that lands after the cuts, so it has to travel through
   * the frame that covers it.
   */
  const seekReal = (atMs: number) => {
    if (frames.length === 0) return;
    seek(playPos(atMs));
    setPlaying(false);
  };

  /**
   * openSpan plays a cut stretch in full. The timeline changes underneath, so
   * the playhead is moved to the start of that stretch on the new one; leaving
   * it where it was would jump to an unrelated moment.
   */
  const openSpan = (span: number) => {
    const next = new Set(opened);
    next.add(span);
    setOpened(next);
    const after = timeline(frames, act, (s) => skipIdle && !next.has(s));
    const start = after[act.spans[span].from] ?? 0;
    posRef.current = start;
    setPos(start);
    drawnRef.current = -1;
  };

  /**
   * At the end the button says Replay and starts from the top. A Play button
   * that is already at the end has nothing to play, and pressing it and getting
   * nothing reads as a broken player.
   */
  const ended = total > 0 && pos >= total;

  const toggle = () => {
    if (ended && !playing) {
      seek(0);
      setPlaying(true);
      return;
    }
    setPlaying((p) => !p);
  };

  const step = (delta: number) => {
    const i = Math.max(0, Math.min(frames.length - 1, indexAt(posRef.current) + delta));
    seek(playAt[i]);
    setPlaying(false);
  };

  /**
   * jump moves to the next or previous thing that happened. Aiming at a mark
   * in a bar a thousand pixels wide is a game of darts, so the marks are also
   * reachable as a pair of buttons and as a pair of keys.
   */
  const jump = (delta: number) => {
    if (acts.length === 0) return;
    // A tolerance, or a jump forward lands on the mark already under the head.
    const here = posRef.current;
    const next =
      delta > 0
        ? acts.find((m) => m.at > here + 1)
        : [...acts].reverse().find((m) => m.at < here - 1);
    if (!next) return;
    seek(next.at);
    setPlaying(false);
  };

  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if (ev.target instanceof HTMLInputElement) return;
      if (ev.key === ' ') {
        ev.preventDefault();
        toggle();
      }
      if (ev.key === 'ArrowRight') step(1);
      if (ev.key === 'ArrowLeft') step(-1);
      if (ev.key === 'ArrowDown') jump(1);
      if (ev.key === 'ArrowUp') jump(-1);
      if (ev.key === 'Escape') onBack();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  });

  if (error) {
    return (
      <div className="player">
        <div className="banner error">{error}</div>
        <button type="button" className="btn" onClick={onBack}>
          Back
        </button>
      </div>
    );
  }
  if (!manifest) return <div className="player">Loading …</div>;

  return (
    <div className="player">
      <div className="player-head">
        <button type="button" className="btn" onClick={onBack}>
          ← Recordings
        </button>
        <h1>{manifest.title || manifest.id}</h1>
        <span className="dim small">
          {new Date(manifest.startedAt).toLocaleString()} · {clock(manifest.durationMs)} ·{' '}
          {frames.length} frames · {humanBytes(manifest.bytes)} · {manifest.browser}
        </span>
      </div>

      {/* The dock sits beside the stage, not inside it: the stage is a size
          container, and a sibling inside it would be measured as part of the
          space the picture is allowed to fill. */}
      <div className="work">
        <div className="stage fit">
          <FrameCanvas
            next={nextBitmap}
            onDrawn={keepBitmap}
            className="viewport"
          />
        </div>
        {dock && (
          <DevTools
            rows={log}
            limit={seen}
            atMs={realMs}
            onSeek={seekReal}
            onClose={() => setDock(false)}
          />
        )}
      </div>

      <div className="timeline">
        <Scrubber
          frames={frames}
          marks={acts}
          act={act}
          playAt={playAt}
          total={total}
          pos={pos}
          collapsed={collapsed}
          onSeek={seek}
          onExpand={openSpan}
        />
      </div>

      <div className="controls">
        <button
          type="button"
          className="btn btn-icon"
          title="Back one frame (left arrow)"
          aria-label="Back one frame"
          onClick={() => step(-1)}
        >
          <Icon name="stepBack" />
        </button>
        <button type="button" className="btn btn-primary" onClick={toggle}>
          <Icon name={playing ? 'pause' : ended ? 'replay' : 'play'} />
          {playing ? 'Pause' : ended ? 'Replay' : 'Play'}
        </button>
        <button
          type="button"
          className="btn btn-icon"
          title="Forward one frame (right arrow)"
          aria-label="Forward one frame"
          onClick={() => step(1)}
        >
          <Icon name="stepForward" />
        </button>

        {/* A recording with no marks gets no jump buttons: two dead controls
            say less than none at all. */}
        {acts.length > 0 && (
          <span className="seg">
            <button
              type="button"
              title="Previous action (up arrow)"
              aria-label="Previous action"
              onClick={() => jump(-1)}
            >
              <Icon name="prevMark" />
            </button>
            <button
              type="button"
              title="Next action (down arrow)"
              aria-label="Next action"
              onClick={() => jump(1)}
            >
              <Icon name="nextMark" />
            </button>
          </span>
        )}

        <span className="dim num">
          {clock(pos)} / {clock(total)}
          {cutMs > 0 && ` (${clock(manifest.durationMs)})`}
        </span>

        <span className="seg">
          {SPEEDS.map((s) => (
            <button
              key={s}
              type="button"
              className={speed === s ? 'on' : ''}
              onClick={() => setSpeed(s)}
            >
              {s}×
            </button>
          ))}
        </span>

        <label
          className={skipIdle ? 'on' : ''}
          title={
            act.scored
              ? 'Cut the stretches where the page did not change'
              : 'This recording has no activity scores, so long pauses are cut instead'
          }
        >
          <input
            type="checkbox"
            checked={skipIdle}
            onChange={(e) => {
              setSkipIdle(e.target.checked);
              setOpened(new Set());
              seek(0);
            }}
          />
          Skip inactivity{cutMs > 0 && ` (−${clock(cutMs)})`}
        </label>

        {/* A recording made before the log existed has no dock. An empty dock
            would claim the page said nothing, which is a different statement. */}
        {manifest.devtools && (
          <>
            <button
              type="button"
              className={`btn${dock ? ' on' : ''}`}
              title="Console, network and issues"
              aria-pressed={dock}
              onClick={() => setDock((d) => !d)}
            >
              <Icon name="console" />
              DevTools
              {manifest.devtools.errors > 0 && (
                <span className="pill bad">{manifest.devtools.errors}</span>
              )}
            </button>
            <button
              type="button"
              className={`btn${showWarnings ? ' on' : ''}`}
              title="Also mark the warnings on the timeline"
              aria-pressed={showWarnings}
              onClick={() => setShowWarnings((w) => !w)}
            >
              <Icon name="warn" />
              Warnings
              {showWarnings && warnMarks.length > 0 && (
                <span className="pill">{warnMarks.length}</span>
              )}
            </button>
          </>
        )}

        <span className="grow" />

        {mp4 ? (
          <a className="btn" href={api.mp4URL(id)} download={`${id}.mp4`}>
            Download MP4
          </a>
        ) : (
          <button
            type="button"
            className="btn"
            disabled={encoding}
            onClick={() => {
              setEncoding(true);
              api
                .encode(id)
                .then(() => setMp4(true))
                .catch((err) => setError(err instanceof Error ? err.message : String(err)))
                .finally(() => setEncoding(false));
            }}
          >
            {encoding ? 'Encoding …' : 'Export MP4'}
          </button>
        )}
      </div>

      {manifest.droppedFrames > 0 && (
        <div className="banner">
          {manifest.droppedFrames} frames were dropped while recording, because the disk
          could not keep up.
        </div>
      )}
    </div>
  );
}
