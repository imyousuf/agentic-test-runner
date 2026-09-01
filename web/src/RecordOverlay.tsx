import { useState } from 'react';
import { api, clock, humanBytes } from './api';
import { Icon } from './Icon';
import { idleRecord, type Elsewhere, type RecordState } from './useLiveView';

interface Props {
  state: RecordState;
  onChange: (next: RecordState) => void;
  onError: (message: string) => void;
}

/**
 * RecordOverlay says, on top of the picture, that the picture is being kept.
 *
 * It sits over the browser view rather than on the toolbar because the toolbar
 * is not where anybody is looking. A person driving a session watches the page,
 * and "am I still recording?" is a question about the page, so the answer
 * belongs on it — the same place a screen recorder puts it.
 *
 * It is the whole recording UI while a recording runs. The toolbar keeps the
 * button that starts one and nothing else, so the state is reported once.
 */
export function RecordOverlay({ state, onChange, onError }: Props) {
  const [busy, setBusy] = useState(false);

  const stop = async () => {
    setBusy(true);
    try {
      await api.stopRecording();
      onChange(idleRecord);
    } catch (err) {
      onError(String(err instanceof Error ? err.message : err));
    } finally {
      setBusy(false);
    }
  };

  if (state.recording) {
    return (
      <div className="rec-overlay" role="status">
        <span className="rec-dot" />
        <span className="rec-label">{state.title || 'Recording'}</span>
        <span className="rec-meta num">
          {clock(state.elapsedMs)} · {state.frames} frames · {humanBytes(state.bytes)}
          {state.dropped > 0 && ` · ${state.dropped} dropped`}
        </span>
        <button type="button" className="btn rec-stop" disabled={busy} onClick={stop}>
          <Icon name="stop" /> Stop
        </button>
      </div>
    );
  }

  // A recording another process owns. It is just as real, and this page cannot
  // stop it, so the overlay reports it and offers no button that would lie.
  if (state.elsewhere.length > 0) {
    const first = state.elsewhere[0];
    return (
      <div
        className="rec-overlay theirs"
        role="status"
        title={state.elsewhere.map((l) => `${l.title || l.id} · ${who(l.source)}`).join('\n')}
      >
        <span className="rec-dot" />
        <span className="rec-label">{first.title || first.id}</span>
        <span className="rec-meta num">{clock(first.elapsedMs)}</span>
        <span className="rec-note">
          {who(first.source)} is recording
          {state.elsewhere.length > 1 && ` · +${state.elsewhere.length - 1} more`}
        </span>
      </div>
    );
  }

  return null;
}

function who(source: Elsewhere['source']): string {
  if (source === 'cli') return 'atr record';
  if (source === 'live-view') return 'Another viewer';
  return source || 'Another process';
}
