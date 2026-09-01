// Wire types shared with internal/remote.

export interface FrameHeader {
  seq: number;
  deviceWidth: number;
  deviceHeight: number;
  pageScale: number;
  offsetTop: number;
  scrollX: number;
  scrollY: number;
}

export interface PageInfo {
  id: string;
  title: string;
  url: string;
  active: boolean;
}

export interface StatusMsg {
  t: 'status';
  streaming: boolean;
  viewers: number;
  viewOnly?: boolean;
  /** False when the server was started without a recordings directory. */
  canRecord?: boolean;
}

export interface PagesMsg {
  t: 'pages';
  pages: PageInfo[];
}

export interface ErrorMsg {
  t: 'error';
  message: string;
}

/** RecordMsg arrives once a second while a recording runs. */
export interface RecordMsg {
  t: 'record';
  recording: boolean;
  id: string;
  title: string;
  elapsedMs: number;
  frames: number;
  bytes: number;
  dropped: number;
  note?: string;
  /** Recordings of this library that another process is writing. */
  elsewhere?: { id: string; title: string; source: string; elapsedMs: number }[];
}

/**
 * LogMsg carries what the page reported. It arrives a row at a time while the
 * page runs, and as one batch when a viewer connects, so the dock has something
 * in it the moment it opens.
 */
export interface LogMsg {
  t: 'log';
  rows: LogEvent[];
}

export type ServerMsg = StatusMsg | PagesMsg | ErrorMsg | RecordMsg | LogMsg;

/** The kinds a LogEvent can be, matching internal/record/devtools.go. */
export type LogKind =
  | 'console'
  | 'error'
  | 'issue'
  | 'req'
  | 'res'
  | 'netfail'
  | 'tap'
  | 'drop';

export type LogLevel = 'debug' | 'info' | 'log' | 'warning' | 'error';

/**
 * LogEvent is one line of devtools.jsonl.
 *
 * It never carries a request body, a response body, or a header. A login post
 * holds the password in its body and the session in its headers.
 */
export interface LogEvent {
  /** Where it sits on the recording clock. */
  atMs: number;
  /** The wall clock the tap stamped, in unix milliseconds. */
  ts: number;
  t: LogKind;
  level?: LogLevel;
  text?: string;
  stack?: string;
  /** Ties a res or a netfail to its req. */
  reqId?: string;
  method?: string;
  url?: string;
  status?: number;
  kind?: string;
  bytes?: number;
  durMs?: number;
  /** How many identical lines this one stands for. Zero and one both mean one. */
  count?: number;
  targetId?: string;
}

/** DevToolsInfo is what the manifest says about devtools.jsonl. */
export interface DevToolsInfo {
  lines: number;
  bytes: number;
  dropped: number;
  /** Failures the page reported, not the marks they made. */
  errors: number;
  bodies: boolean;
  headers: boolean;
  redactQuery: boolean;
}

// Recording types, shared with internal/record.

export interface FrameRecord {
  seq: number;
  file: string;
  atMs: number;
  w: number;
  h: number;
  targetId?: string;
  /**
   * How much this frame differs from the frame one reference lag earlier, 0
   * to 1. Absent in a version 1 recording, and absent when it is zero.
   *
   * `file` is not unique: a run of frames that show the same picture all point
   * at the one file that was written for the run.
   */
  score?: number;
}

export type EventKind =
  | 'tab'
  | 'nav'
  | 'click'
  | 'type'
  | 'key'
  | 'error'
  | 'netfail'
  /**
   * The recorder never writes this one. The player derives it from
   * devtools.jsonl when the warnings chip is on, because a chip that only
   * changed the next recording would not be a chip.
   */
  | 'warn'
  | 'stall'
  | 'resume'
  | 'note';

export interface RecEvent {
  atMs: number;
  t: EventKind;
  targetId?: string;
  url?: string;
  /** What kind of thing it was: a button name, a key name. Never typed text. */
  reason?: string;
  /**
   * How many identical events this one stands for. A broken page reports the
   * same error fifty times in a moment, and fifty marks in one place hide every
   * other mark on the bar. Zero and one both mean one.
   */
  count?: number;
}

export interface Manifest {
  version: number;
  id: string;
  title: string;
  startedAt: string;
  stoppedAt: string;
  durationMs: number;
  browser: string;
  options: {
    quality: number;
    maxWidth: number;
    fps: number;
    policy: string;
    refLagMs?: number;
    /** The threshold the recorder suggests. A player may use another one. */
    changeThreshold?: number;
    dedupeEpsilon?: number;
    keepEveryMs?: number;
  };
  droppedFrames: number;
  /** Frames that point at a file another frame owns. No frame was lost. */
  sharedFrames?: number;
  bytes: number;
  frames: FrameRecord[];
  events: RecEvent[];
  /**
   * Absent only on a recording made before the log existed. A player reads that
   * as "no dock", not as an empty one.
   */
  devtools?: DevToolsInfo;
}

export interface RecordingSummary {
  id: string;
  title: string;
  startedAt: string;
  durationMs: number;
  frames: number;
  bytes: number;
  hasMp4: boolean;
  partial: boolean;
  /** Being written right now, so it has no manifest yet and needs no repair. */
  live?: boolean;
  /** What is writing it: "cli" for atr record, "live-view" for this page. */
  source?: string;
  /** Failures the page reported, so a row can say which session to open. */
  errors?: number;
}

/** Modifier bits, as CDP defines them. */
export function modifiers(ev: {
  altKey: boolean;
  ctrlKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
}): number {
  return (
    (ev.altKey ? 1 : 0) |
    (ev.ctrlKey ? 2 : 0) |
    (ev.metaKey ? 4 : 0) |
    (ev.shiftKey ? 8 : 0)
  );
}

/** The first frame byte marks the message kind. */
export const MSG_FRAME = 0x01;

export function decodeFrame(buf: ArrayBuffer): { header: FrameHeader; jpeg: Uint8Array } | null {
  const view = new DataView(buf);
  if (view.byteLength < 5 || view.getUint8(0) !== MSG_FRAME) return null;
  const headerLen = view.getUint32(1);
  const header = JSON.parse(
    new TextDecoder().decode(new Uint8Array(buf, 5, headerLen)),
  ) as FrameHeader;
  return { header, jpeg: new Uint8Array(buf, 5 + headerLen) };
}
