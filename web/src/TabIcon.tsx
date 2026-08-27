import { useState } from 'react';

/** The monitor glyph, also used as the application's own favicon. */
function Glyph() {
  return (
    <svg className="tab-icon" viewBox="0 0 32 32" aria-hidden="true">
      <rect
        x="6.25"
        y="7.25"
        width="19.5"
        height="13.5"
        rx="2.25"
        fill="none"
        stroke="currentColor"
        strokeWidth="3"
      />
      <path
        d="M16 20.75V25M11.5 25h9"
        fill="none"
        stroke="currentColor"
        strokeWidth="3"
        strokeLinecap="round"
      />
    </svg>
  );
}

/**
 * A tab's favicon, guessed from its origin.
 *
 * Guessed rather than asked for: reading the real icon means evaluating script
 * in a page the viewer does not own, on every tab, every time the list
 * refreshes. The viewer's own browser fetches /favicon.ico instead, so nothing
 * touches the remote page, and a tab whose origin is unreachable from here — an
 * internal host behind the agent's network, say — simply keeps the glyph.
 */
export function TabIcon({ url }: { url: string }) {
  const [failed, setFailed] = useState(false);

  let href = '';
  try {
    const u = new URL(url);
    if (u.protocol === 'http:' || u.protocol === 'https:') {
      href = new URL('/favicon.ico', u.origin).href;
    }
  } catch {
    // about:blank, chrome://…, and a blank tab have no origin to guess from.
  }

  if (!href || failed) return <Glyph />;

  return (
    <img
      className="tab-icon"
      src={href}
      alt=""
      loading="lazy"
      onError={() => setFailed(true)}
    />
  );
}
