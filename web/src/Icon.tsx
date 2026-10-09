// Icons are drawn, not typed.
//
// The controls used to be text: ⏮ U+23EE, ⏭ U+23ED and ⏸ U+23F8. The
// stylesheet asks for system-ui and loads no icon font, and system-ui on Linux
// has no glyph for any of the three, so they drew as empty boxes. ▶ survived,
// which is why Play looked right and the step buttons did not.
//
// An SVG has no such gap. Each one sizes at 1em and paints with currentColor,
// so it follows the button it sits in, on every platform.

interface Props {
  name: Name;
  /** Overrides the size, which is otherwise the text size of the button. */
  size?: number | string;
}

export type Name =
  | 'play'
  | 'pause'
  | 'replay'
  | 'stepBack'
  | 'stepForward'
  | 'prevMark'
  | 'nextMark'
  | 'record'
  | 'stop'
  | 'console'
  | 'warn'
  | 'auto'
  | 'light'
  | 'dark';

export function Icon({ name, size }: Props) {
  return (
    <svg
      className="icon"
      viewBox="0 0 24 24"
      width={size ?? '1em'}
      height={size ?? '1em'}
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {PATHS[name]}
    </svg>
  );
}

// A filled shape reads better than an outline at 14 px, so the transport
// glyphs paint their body and the rest stay as strokes.
const PATHS: Record<Name, React.ReactNode> = {
  play: <path d="M7 4.5 20 12 7 19.5Z" fill="currentColor" stroke="none" />,
  pause: (
    <>
      <rect x="6" y="4.5" width="4" height="15" rx="1" fill="currentColor" stroke="none" />
      <rect x="14" y="4.5" width="4" height="15" rx="1" fill="currentColor" stroke="none" />
    </>
  ),
  // An arrow round a circle, with the play triangle inside it: start again.
  replay: (
    <>
      <path d="M20 12a8 8 0 1 1-2.6-5.9" />
      <path d="M20 3.5V9h-5.5" />
      <path d="M10.5 9.2 15 12l-4.5 2.8Z" fill="currentColor" stroke="none" />
    </>
  ),
  stepBack: (
    <>
      <path d="M20 5.5 9 12l11 6.5Z" fill="currentColor" stroke="none" />
      <rect x="4" y="5" width="3" height="14" rx="1" fill="currentColor" stroke="none" />
    </>
  ),
  stepForward: (
    <>
      <path d="M4 5.5 15 12 4 18.5Z" fill="currentColor" stroke="none" />
      <rect x="17" y="5" width="3" height="14" rx="1" fill="currentColor" stroke="none" />
    </>
  ),
  prevMark: (
    <>
      <path d="M17 5.5 8 12l9 6.5Z" fill="currentColor" stroke="none" />
      <path d="M4 4v16" />
    </>
  ),
  nextMark: (
    <>
      <path d="M7 5.5 16 12l-9 6.5Z" fill="currentColor" stroke="none" />
      <path d="M20 4v16" />
    </>
  ),
  record: <circle cx="12" cy="12" r="6" fill="currentColor" stroke="none" />,
  stop: <rect x="6" y="6" width="12" height="12" rx="2" fill="currentColor" stroke="none" />,
  // A prompt: the caret and the line beside it, as every console draws it.
  console: (
    <>
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M7 9.5 10 12l-3 2.5M13 15h4" />
    </>
  ),
  warn: (
    <>
      <path d="M12 3.5 22 20H2Z" />
      <path d="M12 10v4M12 17h.01" />
    </>
  ),
  auto: (
    <>
      <circle cx="12" cy="12" r="8" />
      <path d="M12 4a8 8 0 0 0 0 16Z" fill="currentColor" stroke="none" />
    </>
  ),
  light: (
    <>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M19.1 4.9l-1.4 1.4M6.3 17.7l-1.4 1.4" />
    </>
  ),
  dark: <path d="M20 14.5A8.5 8.5 0 0 1 9.5 4a8.5 8.5 0 1 0 10.5 10.5Z" />,
};
