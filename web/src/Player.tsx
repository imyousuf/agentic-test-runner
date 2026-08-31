import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { api, clock, humanBytes } from './api';
import { FrameCanvas } from './FrameCanvas';
import type { Manifest } from './protocol';

/** A pause longer than this is a gap worth skipping. */
const GAP_MS = 2000;
/** A skipped gap is replayed as this much, so the cut is still visible. */
const GAP_SHOWN_MS = 500;
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
  const [skipGaps, setSkipGaps] = useState(false);
  const [pos, setPos] = useState(0);
  const [encoding, setEncoding] = useState(false);
  const [mp4, setMp4] = useState(false);

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
    return () => {
      live = false;
    };
  }, [id]);

  const frames = manifest?.frames ?? [];

  /**
   * playAt maps each frame to its position on the playback timeline. With
   * "skip gaps" on, a long pause is compressed, so the timeline is no longer
   * the recording clock and every lookup has to go through this array.
   */
  const playAt = useMemo(() => {
    const out = new Array<number>(frames.length);
    let t = 0;
    for (let i = 0; i < frames.length; i += 1) {
      if (i > 0) {
        let d = frames[i].atMs - frames[i - 1].atMs;
        if (skipGaps && d > GAP_MS) d = GAP_SHOWN_MS;
        t += d;
      }
      out[i] = t;
    }
    return out;
  }, [frames, skipGaps]);

  const total = playAt.length > 0 ? playAt[playAt.length - 1] + GAP_SHOWN_MS : 0;

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

  const step = (delta: number) => {
    const i = Math.max(0, Math.min(frames.length - 1, indexAt(posRef.current) + delta));
    seek(playAt[i]);
    setPlaying(false);
  };

  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if (ev.target instanceof HTMLInputElement) return;
      if (ev.key === ' ') {
        ev.preventDefault();
        setPlaying((p) => !p);
      }
      if (ev.key === 'ArrowRight') step(1);
      if (ev.key === 'ArrowLeft') step(-1);
      if (ev.key === 'Escape') onBack();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  });

  if (error) {
    return (
      <div className="player">
        <div className="banner error">{error}</div>
        <button type="button" onClick={onBack}>
          Back
        </button>
      </div>
    );
  }
  if (!manifest) return <div className="player">Loading …</div>;

  const gaps = skipGaps
    ? 0
    : frames.reduce(
        (n, f, i) => (i > 0 && f.atMs - frames[i - 1].atMs > GAP_MS ? n + 1 : n),
        0,
      );

  return (
    <div className="player">
      <div className="player-head">
        <button type="button" onClick={onBack}>
          ← Recordings
        </button>
        <h1>{manifest.title || manifest.id}</h1>
        <span className="dim small">
          {new Date(manifest.startedAt).toLocaleString()} · {clock(manifest.durationMs)} ·{' '}
          {frames.length} frames · {humanBytes(manifest.bytes)} · {manifest.browser}
        </span>
      </div>

      <div className="stage">
        <FrameCanvas
          next={nextBitmap}
          onDrawn={keepBitmap}
          className="viewport"
        />
      </div>

      <div className="timeline">
        <input
          type="range"
          min={0}
          max={Math.max(1, total)}
          value={pos}
          onChange={(e) => seek(Number(e.target.value))}
        />
        <div className="ticks">
          {manifest.events.map((ev, i) => {
            const at = playAt[frameAt(frames, ev.atMs)] ?? 0;
            return (
              <span
                key={i}
                className={`tick ${ev.t}`}
                style={{ left: `${total > 0 ? (at / total) * 100 : 0}%` }}
                title={`${clock(ev.atMs)} ${ev.t}${ev.url ? ` · ${ev.url}` : ''}${
                  ev.reason ? ` · ${ev.reason}` : ''
                }`}
              />
            );
          })}
        </div>
      </div>

      <div className="controls">
        <button type="button" onClick={() => step(-1)}>
          ⏮
        </button>
        <button type="button" className="primary" onClick={() => setPlaying((p) => !p)}>
          {playing ? '⏸ Pause' : '▶ Play'}
        </button>
        <button type="button" onClick={() => step(1)}>
          ⏭
        </button>
        <span className="dim">
          {clock(pos)} / {clock(total)}
        </span>

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

        <label className={skipGaps ? 'on' : ''}>
          <input
            type="checkbox"
            checked={skipGaps}
            onChange={(e) => {
              setSkipGaps(e.target.checked);
              seek(0);
            }}
          />
          Skip gaps over 2 s{gaps > 0 && ` (${gaps})`}
        </label>

        <span className="grow" />

        {mp4 ? (
          <a href={api.mp4URL(id)} download={`${id}.mp4`}>
            Download MP4
          </a>
        ) : (
          <button
            type="button"
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

/** frameAt finds the frame that covers a moment in recording time. */
function frameAt(frames: { atMs: number }[], atMs: number): number {
  let lo = 0;
  let hi = frames.length - 1;
  let best = 0;
  while (lo <= hi) {
    const mid = (lo + hi) >> 1;
    if (frames[mid].atMs <= atMs) {
      best = mid;
      lo = mid + 1;
    } else {
      hi = mid - 1;
    }
  }
  return best;
}
