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
}

export type ServerMsg = StatusMsg | PagesMsg | ErrorMsg | RecordMsg;

// Recording types, shared with internal/record.

export interface FrameRecord {
  seq: number;
  file: string;
  atMs: number;
  w: number;
  h: number;
  targetId?: string;
}

export interface RecEvent {
  atMs: number;
  t: 'tab' | 'stall' | 'resume' | 'note';
  targetId?: string;
  url?: string;
  reason?: string;
}

export interface Manifest {
  version: number;
  id: string;
  title: string;
  startedAt: string;
  stoppedAt: string;
  durationMs: number;
  browser: string;
  options: { quality: number; maxWidth: number; fps: number; policy: string };
  droppedFrames: number;
  bytes: number;
  frames: FrameRecord[];
  events: RecEvent[];
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
