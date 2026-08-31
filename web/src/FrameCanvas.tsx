import { useEffect, useRef, type CSSProperties, type ReactNode } from 'react';

interface Props {
  /**
   * next returns the bitmap to draw, or null when nothing changed. It is
   * called once per animation frame, so both the live view and the player draw
   * at most one image per repaint and skip anything older.
   */
  next: () => ImageBitmap | null;
  /** onDrawn releases a bitmap the canvas has finished with. The live view
   * closes it; the player keeps it in its prefetch cache. */
  onDrawn?: (bitmap: ImageBitmap) => void;
  className?: string;
  style?: CSSProperties;
  children?: ReactNode;
  canvasRef?: React.RefObject<HTMLCanvasElement | null>;
  tabIndex?: number;
  handlers?: Partial<React.DOMAttributes<HTMLCanvasElement>>;
}

/** FrameCanvas draws the newest bitmap once per animation frame. */
export function FrameCanvas({
  next,
  onDrawn,
  className,
  style,
  canvasRef,
  tabIndex,
  handlers,
}: Props) {
  const ownRef = useRef<HTMLCanvasElement>(null);
  const ref = canvasRef ?? ownRef;

  useEffect(() => {
    let running = true;
    const draw = () => {
      if (!running) return;
      const canvas = ref.current;
      const bitmap = next();
      if (canvas && bitmap) {
        if (canvas.width !== bitmap.width || canvas.height !== bitmap.height) {
          canvas.width = bitmap.width;
          canvas.height = bitmap.height;
        }
        canvas.getContext('2d')?.drawImage(bitmap, 0, 0);
        if (onDrawn) onDrawn(bitmap);
        else bitmap.close();
      }
      requestAnimationFrame(draw);
    };
    requestAnimationFrame(draw);
    return () => {
      running = false;
    };
  }, [next, onDrawn, ref]);

  return (
    <canvas ref={ref} className={className} style={style} tabIndex={tabIndex} {...handlers} />
  );
}
