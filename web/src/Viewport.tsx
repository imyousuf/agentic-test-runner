import { useEffect, useRef } from 'react';
import { FrameCanvas } from './FrameCanvas';
import { modifiers, type FrameHeader } from './protocol';

interface Props {
  takeFrame: () => ImageBitmap | null;
  header: React.RefObject<FrameHeader | null>;
  send: (msg: unknown) => void;
  disabled: boolean;
}

const BUTTONS = ['left', 'middle', 'right'] as const;

/** How close in time and space two presses must be to count as a double click. */
const MULTI_CLICK_MS = 500;
const MULTI_CLICK_SLOP = 5;

/** CDP's `buttons` bitmask, which is not the same numbering as `button`. */
const BUTTON_BIT = [1, 4, 2] as const;

/**
 * Paste is the only shortcut whose default action we need: cancelling the
 * keydown would suppress the paste event that onPaste turns into insertText.
 * Copy and cut are forwarded like any other key, so the remote page still sees
 * them.
 */
function isPasteShortcut(ev: React.KeyboardEvent): boolean {
  return (ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === 'v';
}

export function Viewport({ takeFrame, header, send, disabled }: Props) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const moveQueued = useRef(false);
  // PointerEvent.detail is always 0, so the click count has to be tracked here
  // or Input.dispatchMouseEvent never sees a double click.
  const lastClick = useRef({ time: 0, x: 0, y: 0, count: 0, button: -1 });
  // Chrome reads a mouseMoved with no held buttons as a hover, so a drag needs
  // the held set forwarded on every move.
  const held = useRef(0);

  // Draw the newest frame once per animation frame. Older frames are skipped.
  useEffect(() => {
    let running = true;
    const draw = () => {
      if (!running) return;
      const canvas = canvasRef.current;
      const bitmap = takeFrame();
      if (canvas && bitmap) {
        if (canvas.width !== bitmap.width || canvas.height !== bitmap.height) {
          canvas.width = bitmap.width;
          canvas.height = bitmap.height;
        }
        const ctx = canvas.getContext('2d');
        ctx?.drawImage(bitmap, 0, 0);
        bitmap.close();
      }
      requestAnimationFrame(draw);
    };
    requestAnimationFrame(draw);
    return () => {
      running = false;
    };
  }, [takeFrame]);

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
    // Capture the pointer so a drag that leaves the canvas still delivers its
    // pointerup here. Without it the remote button stays pressed forever.
    canvasRef.current?.setPointerCapture(ev.pointerId);

    const prev = lastClick.current;
    const near =
      Math.abs(ev.clientX - prev.x) <= MULTI_CLICK_SLOP &&
      Math.abs(ev.clientY - prev.y) <= MULTI_CLICK_SLOP;
    const sameButton = ev.button === prev.button;
    const consecutive = ev.timeStamp - prev.time <= MULTI_CLICK_MS && near && sameButton;
    const count = consecutive ? prev.count + 1 : 1;
    lastClick.current = {
      time: ev.timeStamp,
      x: ev.clientX,
      y: ev.clientY,
      count,
      button: ev.button,
    };

    held.current |= BUTTON_BIT[ev.button] ?? 1;

    const p = toPage(ev.clientX, ev.clientY);
    send({
      t: 'mouse',
      kind: 'pressed',
      x: p.x,
      y: p.y,
      button: BUTTONS[ev.button] ?? 'left',
      clicks: count,
      buttons: held.current,
      mod: modifiers(ev),
    });
  };

  const onPointerUp = (ev: React.PointerEvent<HTMLCanvasElement>) => {
    if (disabled) return;
    if (canvasRef.current?.hasPointerCapture(ev.pointerId)) {
      canvasRef.current.releasePointerCapture(ev.pointerId);
    }
    held.current &= ~(BUTTON_BIT[ev.button] ?? 1);

    const p = toPage(ev.clientX, ev.clientY);
    send({
      t: 'mouse',
      kind: 'released',
      x: p.x,
      y: p.y,
      button: BUTTONS[ev.button] ?? 'left',
      // Match the press: a release must not report a higher count than the
      // press that opened the click.
      clicks: lastClick.current.count || 1,
      buttons: held.current,
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
      send({
        t: 'mouse',
        kind: 'moved',
        x: p.x,
        y: p.y,
        button: '',
        clicks: 0,
        buttons: held.current,
        mod,
      });
    });
  };

  // pointercancel fires instead of pointerup when the browser takes the pointer
  // (a touch gesture claimed for panning, a system dialog, a backgrounded tab).
  // Capture is released implicitly and no pointerup arrives, so the release has
  // to be sent from here or the remote button stays down.
  const onPointerCancel = (ev: React.PointerEvent<HTMLCanvasElement>) => {
    if (disabled) return;
    // pointercancel reports button === -1, so the button that is actually down
    // has to come from the press. Releasing "left" here would leave a middle or
    // right drag stuck.
    const button = lastClick.current.button;
    held.current &= ~(BUTTON_BIT[button] ?? 1);

    const p = toPage(ev.clientX, ev.clientY);
    send({
      t: 'mouse',
      kind: 'released',
      x: p.x,
      y: p.y,
      button: BUTTONS[button] ?? 'left',
      clicks: lastClick.current.count || 1,
      buttons: held.current,
      mod: modifiers(ev),
    });
  };

  const onWheel = (ev: React.WheelEvent<HTMLCanvasElement>) => {
    if (disabled) return;
    const p = toPage(ev.clientX, ev.clientY);
    send({
      t: 'wheel',
      x: p.x,
      y: p.y,
      dx: ev.deltaX,
      dy: ev.deltaY,
      buttons: held.current,
      mod: modifiers(ev),
    });
  };

  const onKeyDown = (ev: React.KeyboardEvent<HTMLCanvasElement>) => {
    if (disabled) return;
    // Paste is the default action of the shortcut, so cancelling the keydown
    // would stop the paste event from firing. Let the browser run it and do not
    // forward the shortcut either, or the remote page would act on its own
    // clipboard as well as the text onPaste sends.
    if (isPasteShortcut(ev)) return;
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
    if (isPasteShortcut(ev)) return;
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
        onPointerCancel,
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
