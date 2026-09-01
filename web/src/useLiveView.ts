import { useCallback, useEffect, useRef, useState } from 'react';
import {
  decodeFrame,
  type FrameHeader,
  type LogEvent,
  type PageInfo,
  type ServerMsg,
} from './protocol';

/**
 * How many log rows the live dock keeps. The server holds the same number, so
 * a viewer who connects late sees exactly what one who was there sees. A live
 * view runs for hours, and the whole log of those hours is on disk in the
 * recording, not in a tab.
 */
const LOG_RING = 2000;

/**
 * How often the arrived rows are moved into state. The tap lets through up to
 * two hundred rows a second, and a render for each one would cost more than
 * the frames do.
 */
const LOG_FLUSH_MS = 300;

/** A recording another process is writing, such as one "atr record" started. */
export interface Elsewhere {
  id: string;
  title: string;
  source: string;
  elapsedMs: number;
}

export interface RecordState {
  recording: boolean;
  id: string;
  title: string;
  elapsedMs: number;
  frames: number;
  bytes: number;
  dropped: number;
  note: string;
  /**
   * Recordings of this library that somebody else is writing. This server did
   * not start them and cannot stop them, but a person watching the live view
   * still has to know that what is on the screen is being kept.
   */
  elsewhere: Elsewhere[];
}

export const idleRecord: RecordState = {
  recording: false,
  id: '',
  title: '',
  elapsedMs: 0,
  frames: 0,
  bytes: 0,
  dropped: 0,
  note: '',
  elsewhere: [],
};

export interface LiveState {
  connected: boolean;
  streaming: boolean;
  viewers: number;
  viewOnly: boolean;
  canRecord: boolean;
  pages: PageInfo[];
  fps: number;
  error: string;
  record: RecordState;
  /** What the page has reported, newest last, capped at LOG_RING rows. */
  log: LogEvent[];
}

/**
 * useLiveView owns the socket. Frames go into a ref, not into state: a frame
 * arrives up to 60 times a second, and a React render for each one would waste
 * the whole frame budget.
 */
export function useLiveView() {
  const [state, setState] = useState<LiveState>({
    connected: false,
    streaming: false,
    viewers: 0,
    viewOnly: false,
    canRecord: false,
    pages: [],
    fps: 0,
    error: '',
    record: idleRecord,
    log: [],
  });

  const socket = useRef<WebSocket | null>(null);
  const pending = useRef<ImageBitmap | null>(null);
  const header = useRef<FrameHeader | null>(null);
  const counter = useRef(0);
  const inbox = useRef<LogEvent[]>([]);

  const send = useCallback((msg: unknown) => {
    const ws = socket.current;
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(msg));
    }
  }, []);

  useEffect(() => {
    const token = new URLSearchParams(location.search).get('t') ?? '';
    const scheme = location.protocol === 'https:' ? 'wss' : 'ws';
    const ws = new WebSocket(`${scheme}://${location.host}/ws?t=${encodeURIComponent(token)}`);
    ws.binaryType = 'arraybuffer';
    socket.current = ws;

    ws.onopen = () => setState((s) => ({ ...s, connected: true, error: '' }));
    ws.onclose = () => setState((s) => ({ ...s, connected: false, streaming: false }));

    ws.onmessage = (ev) => {
      if (typeof ev.data === 'string') {
        const msg = JSON.parse(ev.data) as ServerMsg;
        if (msg.t === 'pages') setState((s) => ({ ...s, pages: msg.pages }));
        if (msg.t === 'status') {
          setState((s) => ({
            ...s,
            streaming: msg.streaming,
            viewers: msg.viewers,
            viewOnly: msg.viewOnly ?? s.viewOnly,
            canRecord: msg.canRecord ?? s.canRecord,
          }));
        }
        if (msg.t === 'record') {
          setState((s) => ({
            ...s,
            record: {
              recording: msg.recording,
              id: msg.id,
              title: msg.title,
              elapsedMs: msg.elapsedMs,
              frames: msg.frames,
              bytes: msg.bytes,
              dropped: msg.dropped,
              note: msg.note ?? '',
              elsewhere: msg.elsewhere ?? [],
            },
          }));
        }
        // The rows queue in a ref and land in state on the flush, so a page
        // that logs in a loop cannot drive the render rate.
        if (msg.t === 'log') inbox.current.push(...msg.rows);
        if (msg.t === 'error') setState((s) => ({ ...s, error: msg.message }));
        return;
      }

      const frame = decodeFrame(ev.data as ArrayBuffer);
      if (!frame) return;
      header.current = frame.header;
      counter.current += 1;

      // createImageBitmap decodes off the main thread.
      createImageBitmap(new Blob([frame.jpeg as BufferSource], { type: 'image/jpeg' }))
        .then((bitmap) => {
          // Drop the previous undrawn bitmap so memory cannot grow.
          pending.current?.close();
          pending.current = bitmap;
          setState((s) => (s.streaming ? s : { ...s, streaming: true }));
        })
        .catch(() => undefined);
    };

    const meter = setInterval(() => {
      const fps = counter.current;
      counter.current = 0;
      setState((s) => (s.fps === fps ? s : { ...s, fps }));
    }, 1000);

    const flush = setInterval(() => {
      const rows = inbox.current;
      if (rows.length === 0) return;
      inbox.current = [];
      setState((s) => {
        const next = s.log.concat(rows);
        return { ...s, log: next.length > LOG_RING ? next.slice(-LOG_RING) : next };
      });
    }, LOG_FLUSH_MS);

    return () => {
      clearInterval(meter);
      clearInterval(flush);
      ws.close();
      pending.current?.close();
      pending.current = null;
    };
  }, []);

  /** takeFrame hands the newest bitmap to the renderer, once. */
  const takeFrame = useCallback(() => {
    const bitmap = pending.current;
    pending.current = null;
    return bitmap;
  }, []);

  /**
   * setRecord lets the button show the new state at once. The server confirms
   * it within a second, so this only removes the wait, it never invents state.
   */
  const setRecord = useCallback((record: RecordState) => {
    setState((s) => ({ ...s, record }));
  }, []);

  /**
   * clearError dismisses the last thing the server refused. Nothing else clears
   * it, and a refusal is about one action, so without this the banner outlives
   * the moment it describes.
   */
  const clearError = useCallback(() => {
    setState((s) => (s.error === '' ? s : { ...s, error: '' }));
  }, []);

  return { state, send, takeFrame, header, setRecord, clearError };
}
