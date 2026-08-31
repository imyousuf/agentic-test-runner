import { useState } from 'react';
import { api, clock, humanBytes } from './api';
import { useRecordings } from './useRecordings';
import type { RecordingSummary } from './protocol';

interface Props {
  onOpen: (id: string) => void;
}

/** Library lists what has been recorded, newest first. */
export function Library({ onOpen }: Props) {
  const { items, loading, error, reload, setError } = useRecordings(true);
  const [busy, setBusy] = useState('');
  const [editing, setEditing] = useState('');
  const [draft, setDraft] = useState('');

  const act = async (id: string, work: () => Promise<unknown>) => {
    setBusy(id);
    try {
      await work();
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy('');
    }
  };

  if (loading) return <div className="library">Loading …</div>;

  return (
    <div className="library">
      <div className="library-head">
        <h1>Recordings</h1>
        <button type="button" onClick={() => void reload()}>
          Refresh
        </button>
      </div>

      {error && <div className="banner error">{error}</div>}

      {items.length === 0 && (
        <p className="dim">
          Nothing recorded yet. Go back to the live view and press ● Record.
        </p>
      )}

      <ul className="rec-list">
        {items.map((r: RecordingSummary) => (
          <li key={r.id} className={busy === r.id ? 'busy' : ''}>
            <div className="rec-main">
              {editing === r.id ? (
                <form
                  onSubmit={(ev) => {
                    ev.preventDefault();
                    setEditing('');
                    void act(r.id, () => api.rename(r.id, draft.trim()));
                  }}
                >
                  <input autoFocus value={draft} onChange={(e) => setDraft(e.target.value)} />
                  <button type="submit">Save</button>
                  <button type="button" onClick={() => setEditing('')}>
                    Cancel
                  </button>
                </form>
              ) : (
                <button className="link" type="button" onClick={() => onOpen(r.id)}>
                  {r.title || r.id}
                </button>
              )}
              <div className="dim small">
                {new Date(r.startedAt).toLocaleString()} · {clock(r.durationMs)} ·{' '}
                {r.frames} frames · {humanBytes(r.bytes)}
                {r.hasMp4 && ' · mp4'}
              </div>
              {r.partial && (
                <div className="warn small">
                  This recording was interrupted, so it has no manifest yet.
                </div>
              )}
            </div>

            <div className="rec-actions">
              {r.partial ? (
                <button type="button" onClick={() => void act(r.id, () => api.repair(r.id))}>
                  Repair
                </button>
              ) : (
                <>
                  <button type="button" onClick={() => onOpen(r.id)}>
                    Play
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setEditing(r.id);
                      setDraft(r.title);
                    }}
                  >
                    Rename
                  </button>
                  {r.hasMp4 ? (
                    <a href={api.mp4URL(r.id)} download={`${r.id}.mp4`}>
                      Download
                    </a>
                  ) : (
                    <button type="button" onClick={() => void act(r.id, () => api.encode(r.id))}>
                      Export MP4
                    </button>
                  )}
                </>
              )}
              <button
                type="button"
                className="danger"
                onClick={() => {
                  if (confirm(`Delete ${r.title || r.id}?`)) {
                    void act(r.id, () => api.remove(r.id));
                  }
                }}
              >
                Delete
              </button>
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}
