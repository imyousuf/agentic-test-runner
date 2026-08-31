import { useState } from 'react';
import { api, clock, humanBytes } from './api';
import { idleRecord, type RecordState } from './useLiveView';

interface Props {
  state: RecordState;
  canRecord: boolean;
  onChange: (next: RecordState) => void;
  onError: (message: string) => void;
}

/**
 * RecordButton starts and stops a recording.
 *
 * Recording is off until somebody presses this. Nothing in ATR turns it on by
 * itself, so a person who opens the live view is never recorded without having
 * asked for it.
 */
export function RecordButton({ state, canRecord, onChange, onError }: Props) {
  const [busy, setBusy] = useState(false);
  const [asking, setAsking] = useState(false);
  const [title, setTitle] = useState('');

  if (!canRecord) return null;

  const start = async (withTitle: string) => {
    setBusy(true);
    setAsking(false);
    try {
      const { id } = await api.startRecording(withTitle);
      onChange({ ...idleRecord, recording: true, id, title: withTitle });
    } catch (err) {
      onError(String(err instanceof Error ? err.message : err));
    } finally {
      setBusy(false);
      setTitle('');
    }
  };

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
      <div className="rec">
        <button type="button" className="rec-btn on" disabled={busy} onClick={stop}>
          <span className="rec-dot" /> Stop
        </button>
        <span className="rec-meta">
          {clock(state.elapsedMs)} · {state.frames} frames · {humanBytes(state.bytes)}
          {state.dropped > 0 && ` · ${state.dropped} dropped`}
        </span>
      </div>
    );
  }

  if (asking) {
    return (
      <form
        className="rec"
        onSubmit={(ev) => {
          ev.preventDefault();
          void start(title.trim());
        }}
      >
        <input
          autoFocus
          value={title}
          placeholder="Name this recording (optional)"
          onChange={(e) => setTitle(e.target.value)}
          onKeyDown={(e) => e.key === 'Escape' && setAsking(false)}
        />
        <button type="submit" className="rec-btn" disabled={busy}>
          <span className="rec-dot" /> Start
        </button>
        <button type="button" onClick={() => setAsking(false)}>
          Cancel
        </button>
      </form>
    );
  }

  return (
    <button
      type="button"
      className="rec-btn"
      disabled={busy}
      title="Record this session"
      onClick={() => setAsking(true)}
    >
      <span className="rec-dot" /> Record
    </button>
  );
}
