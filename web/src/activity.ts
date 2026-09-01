// Activity turns a recording into stretches where something happened and
// stretches where nothing did.
//
// The recorder writes a change score per frame (manifest version 2). The score
// compares a frame with the frame one reference lag earlier, which cancels
// anything that reverts, such as a blinking caret, and keeps anything
// cumulative, such as typing. A version 1 recording has no score, so the old
// rule applies there: a long pause between frames is the only evidence of
// inactivity we have.

import type { EventKind, FrameRecord, Manifest, RecEvent } from './protocol';

/** In a version 1 recording, a pause longer than this is the only idle signal. */
export const FALLBACK_GAP_MS = 2000;
/** A cut idle stretch is replayed as this much, so the cut stays visible. */
export const IDLE_SHOWN_MS = 500;
/** Keep playing for this long after the last thing that moved. */
const TAIL_MS = 1200;
/** Start playing this long before the next thing that moves. */
const LEAD_MS = 500;
/** A quiet stretch shorter than this is not worth cutting. */
const MIN_IDLE_MS = 1500;

export interface Span {
  idle: boolean;
  /** First and last frame index, both inclusive. */
  from: number;
  to: number;
  /** Real recording time this span covers, in milliseconds. */
  realMs: number;
}

export interface Activity {
  /** True when the manifest carries scores, so the spans are real activity. */
  scored: boolean;
  threshold: number;
  /** One score per frame. Zero everywhere in a version 1 recording. */
  scores: number[];
  spans: Span[];
  /** Frame index to span index. */
  spanOf: number[];
  /** Real time inside idle spans. This is what "skip inactivity" removes. */
  idleMs: number;
}

/** analyse splits a recording into active and idle spans. */
export function analyse(manifest: Manifest | null): Activity {
  const frames = manifest?.frames ?? [];
  const threshold = manifest?.options?.changeThreshold || 0.002;
  const empty: Activity = {
    scored: false,
    threshold,
    scores: [],
    spans: [],
    spanOf: [],
    idleMs: 0,
  };
  if (frames.length === 0) return empty;

  const scores = frames.map((f) => f.score ?? 0);
  const scored = (manifest?.version ?? 1) >= 2 && scores.some((s) => s > 0);
  // Hysteresis only makes sense on scores. The version 1 rule already reads a
  // whole pause as one idle frame, and widening that would rescue every pause
  // from its own neighbours and cut nothing at all.
  const kept = scored
    ? withHysteresis(frames, scores.map((s) => s >= threshold))
    : movedByGap(frames);
  const spans = toSpans(frames, kept);

  const spanOf = new Array<number>(frames.length);
  let idleMs = 0;
  spans.forEach((sp, i) => {
    for (let f = sp.from; f <= sp.to; f += 1) spanOf[f] = i;
    if (sp.idle) idleMs += sp.realMs;
  });

  return { scored, threshold, scores, spans, spanOf, idleMs };
}

/**
 * movedByGap is the version 1 rule. Without scores the only thing a manifest
 * says about activity is how far apart the frames are, because the recorder
 * only captured a frame when the page repainted or the heartbeat fired.
 */
function movedByGap(frames: FrameRecord[]): boolean[] {
  return frames.map((f, i) => i === 0 || f.atMs - frames[i - 1].atMs <= FALLBACK_GAP_MS);
}

/**
 * withHysteresis widens each moment of movement into a stretch a person can
 * follow. Cutting on the exact frame reads as a stutter: the eye needs a
 * moment of stillness after a change to take it in, and a moment before the
 * next one to find the place.
 */
function withHysteresis(frames: FrameRecord[], moved: boolean[]): boolean[] {
  const kept = moved.slice();

  let lastMoveMs = -Infinity;
  for (let i = 0; i < frames.length; i += 1) {
    if (moved[i]) lastMoveMs = frames[i].atMs;
    else if (frames[i].atMs - lastMoveMs <= TAIL_MS) kept[i] = true;
  }

  let nextMoveMs = Infinity;
  for (let i = frames.length - 1; i >= 0; i -= 1) {
    if (moved[i]) nextMoveMs = frames[i].atMs;
    else if (nextMoveMs - frames[i].atMs <= LEAD_MS) kept[i] = true;
  }
  return kept;
}

/** toSpans groups the frames into runs, and drops any idle run too short to cut. */
function toSpans(frames: FrameRecord[], kept: boolean[]): Span[] {
  const runs: Span[] = [];
  for (let i = 0; i < frames.length; i += 1) {
    const idle = !kept[i];
    const last = runs[runs.length - 1];
    if (last && last.idle === idle) last.to = i;
    else runs.push({ idle, from: i, to: i, realMs: 0 });
  }

  // A one second pause is not worth a cut; it costs more attention to notice
  // the cut than to watch the second.
  for (const run of runs) run.realMs = realMs(frames, run);
  for (const run of runs) {
    if (run.idle && run.realMs < MIN_IDLE_MS) run.idle = false;
  }

  const merged: Span[] = [];
  for (const run of runs) {
    const last = merged[merged.length - 1];
    if (last && last.idle === run.idle) last.to = run.to;
    else merged.push({ ...run });
  }
  for (const run of merged) run.realMs = realMs(frames, run);
  return merged;
}

/**
 * realMs measures a span from the frame before it, because the pause that made
 * it idle sits on the edge between the two.
 */
function realMs(frames: FrameRecord[], sp: { from: number; to: number }): number {
  const start = frames[Math.max(0, sp.from - 1)].atMs;
  return frames[sp.to].atMs - start;
}

/** Marks closer together than this share of the bar are drawn as one. */
const CLUSTER_SHARE = 1 / 140;

export interface Mark {
  /** Where it sits on the playback clock, in milliseconds. */
  at: number;
  /** The kind drawn for the cluster: the most notable one in it. */
  kind: EventKind;
  events: RecEvent[];
}

/**
 * The kinds, most notable first. A cluster takes the shape of its most notable
 * member.
 *
 * The two failures rank above everything. Somebody scrubbing a session is
 * looking for the moment it broke, and a click that happens to sit beside the
 * error must not hide it.
 */
const RANK: EventKind[] = [
  'error',
  'netfail',
  'warn',
  'nav',
  'tab',
  'key',
  'click',
  'type',
  'stall',
  'resume',
  'note',
];

/** How each kind reads in a tooltip. */
export const KIND_LABEL: Record<EventKind, string> = {
  error: 'error',
  netfail: 'request failed',
  warn: 'warning',
  nav: 'went to',
  tab: 'switched tab to',
  click: 'clicked',
  type: 'typed',
  key: 'pressed',
  stall: 'stalled',
  resume: 'resumed',
  note: 'note',
};

/** The kinds drawn in red, because they are the ones somebody is looking for. */
export function isFailure(kind: EventKind): boolean {
  return kind === 'error' || kind === 'netfail';
}

/**
 * marks places the events on the playback clock and groups the ones that would
 * overlap.
 *
 * They cannot be placed by recording time: cutting the quiet stretches moves
 * everything after the first cut, so a mark has to travel with the frame it
 * belongs to.
 */
export function marks(frames: FrameRecord[], events: RecEvent[], playAt: number[]): Mark[] {
  if (frames.length === 0 || events.length === 0) return [];
  const total = playAt[playAt.length - 1] || 1;
  const gap = total * CLUSTER_SHARE;

  const placed = events
    .map((ev) => ({ ev, at: playAt[frameAt(frames, ev.atMs)] ?? 0 }))
    .sort((a, b) => a.at - b.at);

  const out: Mark[] = [];
  for (const { ev, at } of placed) {
    const last = out[out.length - 1];
    if (last && at - last.at <= gap) {
      last.events.push(ev);
      if (RANK.indexOf(ev.t) < RANK.indexOf(last.kind)) last.kind = ev.t;
      continue;
    }
    out.push({ at, kind: ev.t, events: [ev] });
  }
  return out;
}

/** frameAt finds the frame that covers a moment in recording time. */
export function frameAt(frames: { atMs: number }[], atMs: number): number {
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

/**
 * timeline maps each frame to its position on the playback clock. A collapsed
 * idle span is squeezed to IDLE_SHOWN_MS, spread over its frames in
 * proportion, so a seek inside the stub still lands somewhere sensible.
 */
export function timeline(
  frames: FrameRecord[],
  act: Activity,
  collapsed: (span: number) => boolean,
): number[] {
  const out = new Array<number>(frames.length);
  if (frames.length === 0) return out;

  const scale = act.spans.map((sp, i) =>
    sp.idle && collapsed(i) && sp.realMs > 0 ? IDLE_SHOWN_MS / sp.realMs : 1,
  );

  let t = 0;
  out[0] = 0;
  for (let i = 1; i < frames.length; i += 1) {
    t += (frames[i].atMs - frames[i - 1].atMs) * (scale[act.spanOf[i]] ?? 1);
    out[i] = t;
  }
  return out;
}
