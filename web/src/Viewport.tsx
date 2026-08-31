import { useRef } from 'react';
import { FrameCanvas } from './FrameCanvas';
import { modifiers, type FrameHeader } from './protocol';

interface Props {
  takeFrame: () => ImageBitmap | null;
  header: React.RefObject<FrameHeader | null>;
  send: (msg: unknown) => void;
  disabled: boolean;
}

const BUTTONS = ['left', 'middle', 'right'] as const;

export function Viewport({ takeFrame, header, send, disabled }: Props) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const moveQueued = useRef(false);

  /**
   * Convert a position on the canvas to a page coordinate. The canvas is
   * scaled by CSS, so the ratio has to come from the rendered rectangle, not
   * from the bitmap size.
   */
  const toPage = (clientX: number, clientY: number) => {
    const canvas = canvasRef.current;
    const meta = header.current;
    if (!canvas) return { x: 0, y: 0 };
    const rect = canvas.getBoundingClientRect();
    const width = meta?.deviceWidth || canvas.width;
    const height = meta?.deviceHeight || canvas.height;
    return {
      x: ((clientX - rect.left) / rect.width) * width,
      y: ((clientY - rect.top) / rect.height) * height,
    };
  };

  const onPointerDown = (ev: React.PointerEvent<HTMLCanvasElement>) => {
    canvasRef.current?.focus();
    if (disabled) return;
    const p = toPage(ev.clientX, ev.clientY);
    send({
      t: 'mouse',
      kind: 'pressed',
      x: p.x,
      y: p.y,
      button: BUTTONS[ev.button] ?? 'left',
      clicks: ev.detail || 1,
      mod: modifiers(ev),
    });
  };

  const onPointerUp = (ev: React.PointerEvent<HTMLCanvasElement>) => {
    if (disabled) return;
    const p = toPage(ev.clientX, ev.clientY);
    send({
      t: 'mouse',
      kind: 'released',
      x: p.x,
      y: p.y,
      button: BUTTONS[ev.button] ?? 'left',
      clicks: ev.detail || 1,
      mod: modifiers(ev),
    });
  };

  const onPointerMove = (ev: React.PointerEvent<HTMLCanvasElement>) => {
    if (disabled || moveQueued.current) return;
    moveQueued.current = true;
    const { clientX, clientY } = ev;
    const mod = modifiers(ev);
    requestAnimationFrame(() => {
      moveQueued.current = false;
      const p = toPage(clientX, clientY);
      send({ t: 'mouse', kind: 'moved', x: p.x, y: p.y, button: '', clicks: 0, mod });
    });
  };

  const onWheel = (ev: React.WheelEvent<HTMLCanvasElement>) => {
    if (disabled) return;
    const p = toPage(ev.clientX, ev.clientY);
    send({ t: 'wheel', x: p.x, y: p.y, dx: ev.deltaX, dy: ev.deltaY, mod: modifiers(ev) });
  };

  const onKeyDown = (ev: React.KeyboardEvent<HTMLCanvasElement>) => {
    if (disabled) return;
    // The viewer's own browser would otherwise take Tab, the slash, and the
    // function keys before the page ever sees them.
    ev.preventDefault();
    send({
      t: 'key',
      kind: 'down',
      key: ev.key,
      code: ev.code,
      vk: ev.keyCode,
      text: ev.key.length === 1 ? ev.key : '',
      mod: modifiers(ev),
    });
  };

  const onKeyUp = (ev: React.KeyboardEvent<HTMLCanvasElement>) => {
    if (disabled) return;
    ev.preventDefault();
    send({
      t: 'key',
      kind: 'up',
      key: ev.key,
      code: ev.code,
      vk: ev.keyCode,
      text: '',
      mod: modifiers(ev),
    });
  };

  const onPaste = (ev: React.ClipboardEvent<HTMLCanvasElement>) => {
    if (disabled) return;
    ev.preventDefault();
    send({ t: 'text', value: ev.clipboardData.getData('text') });
  };

  return (
    <FrameCanvas
      next={takeFrame}
      canvasRef={canvasRef}
      className="viewport"
      tabIndex={0}
      handlers={{
        onPointerDown,
        onPointerUp,
        onPointerMove,
        onWheel,
        onKeyDown,
        onKeyUp,
        onPaste,
        onContextMenu: (e) => e.preventDefault(),
      }}
    />
  );
}
