import { useCallback, useEffect, useRef, useState } from 'react';
import { decodeFrame, type FrameHeader, type PageInfo, type ServerMsg } from './protocol';

export interface LiveState {
  connected: boolean;
  streaming: boolean;
  viewers: number;
  viewOnly: boolean;
  pages: PageInfo[];
  fps: number;
  error: string;
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
    pages: [],
    fps: 0,
    error: '',
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
    const url = `${scheme}://${location.host}/ws?t=${encodeURIComponent(token)}`;

    let closed = false;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let attempt = 0;

    // The service restarts on failure and an SSH tunnel can blip, so a dropped
    // socket has to come back on its own. Without this the tab stays dead until
    // someone reloads it by hand.
    const backoff = () => Math.min(500 * 2 ** attempt++, 10_000);

    const connect = () => {
      if (closed) return;
      const ws = new WebSocket(url);
      ws.binaryType = 'arraybuffer';
      socket.current = ws;

      ws.onopen = () => {
        attempt = 0;
        setState((s) => ({ ...s, connected: true, error: '' }));
      };

      ws.onerror = () => {
        setState((s) => (s.connected ? s : { ...s, error: 'Cannot reach the live view server.' }));
      };

      ws.onclose = () => {
        setState((s) => ({ ...s, connected: false, streaming: false }));
        if (!closed) retry = setTimeout(connect, backoff());
      };

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
    };

    connect();

    const meter = setInterval(() => {
      const fps = counter.current;
      counter.current = 0;
      setState((s) => (s.fps === fps ? s : { ...s, fps }));
    }, 1000);

    return () => {
      closed = true;
      clearInterval(meter);
      if (retry) clearTimeout(retry);
      socket.current?.close();
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

  return { state, send, takeFrame, header };
}
