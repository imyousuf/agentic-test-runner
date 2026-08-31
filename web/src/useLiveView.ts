import { useCallback, useEffect, useRef, useState } from 'react';
import { decodeFrame, type FrameHeader, type PageInfo, type ServerMsg } from './protocol';

export interface RecordState {
  recording: boolean;
  id: string;
  title: string;
  elapsedMs: number;
  frames: number;
  bytes: number;
  dropped: number;
  note: string;
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
  });

  const socket = useRef<WebSocket | null>(null);
  const pending = useRef<ImageBitmap | null>(null);
  const header = useRef<FrameHeader | null>(null);
  const counter = useRef(0);

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
            },
          }));
        }
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

    return () => {
      clearInterval(meter);
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

  return { state, send, takeFrame, header, setRecord };
}
