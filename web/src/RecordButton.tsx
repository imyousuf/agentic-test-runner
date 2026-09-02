import { useState } from 'react';
import { api } from './api';
import { Icon } from './Icon';
import { idleRecord, type RecordState } from './useLiveView';

interface Props {
  state: RecordState;
  canRecord: boolean;
  /** Why recording is not offered. Shown on the disabled button. */
  reason?: string;
  onChange: (next: RecordState) => void;
  onError: (message: string) => void;
}

/**
 * RecordButton starts a recording.
 *
 * Recording is off until somebody presses this. Nothing in ATR turns it on by
 * itself, so a person who opens the live view is never recorded without having
 * asked for it.
 *
 * Stopping is not here. Once a recording runs, RecordOverlay owns it: the state
 * belongs over the picture that is being kept, and reporting it in two places
 * would leave two things to keep in step.
 *
 * The button stays on the bar when this page may not record. A control that
 * disappears reads as a missing feature; a disabled one that says why reads as
 * an answer.
 */
export function RecordButton({ state, canRecord, reason, onChange, onError }: Props) {
  const [busy, setBusy] = useState(false);
  const [asking, setAsking] = useState(false);
  const [title, setTitle] = useState('');

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

  // The overlay is showing the running recording and its Stop button, so the
  // bar has nothing to add.
  if (state.recording) return null;

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
          className="field"
          value={title}
          placeholder="What is this recording for?"
          onChange={(e) => setTitle(e.target.value)}
          onKeyDown={(e) => e.key === 'Escape' && setAsking(false)}
        />
        {/* Required, not optional. An untitled recording is a bare timestamp
            in the library, and nobody can tell later what it was for. */}
        <button type="submit" className="btn rec-btn" disabled={busy || title.trim() === ''}>
          <Icon name="record" /> Start
        </button>
        <button type="button" className="btn" onClick={() => setAsking(false)}>
          Cancel
        </button>
      </form>
    );
  }

  return (
    <div className="rec">
      <button
        type="button"
        className="btn rec-btn"
        disabled={busy || !canRecord}
        title={canRecord ? 'Record this session' : reason}
        onClick={() => setAsking(true)}
      >
        <Icon name="record" /> Record
      </button>
    </div>
  );
}
