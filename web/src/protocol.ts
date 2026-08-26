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
}

export interface PagesMsg {
  t: 'pages';
  pages: PageInfo[];
}

export interface ErrorMsg {
  t: 'error';
  message: string;
}

export type ServerMsg = StatusMsg | PagesMsg | ErrorMsg;

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
