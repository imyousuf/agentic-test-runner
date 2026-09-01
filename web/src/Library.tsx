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
        <button type="button" className="btn" onClick={() => void reload()}>
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
        {items.map((r: RecordingSummary) => {
          // A partial recording has no manifest, so there is nothing to play
          // until it is repaired. Renaming makes the row a form, not a target.
          // A live one has no manifest either, and it is not broken: it is
          // still being written, so every action on it has to wait.
          const open = r.partial || r.live || editing === r.id ? undefined : () => onOpen(r.id);
          return (
            <li
              key={r.id}
              className={[busy === r.id ? 'busy' : '', open ? 'openable' : '', r.live ? 'live' : '']
                .join(' ')
                .trim()}
              role={open ? 'button' : undefined}
              tabIndex={open ? 0 : undefined}
              aria-label={open ? `Play ${r.title || r.id}` : undefined}
              onClick={open}
              onKeyDown={(ev) => {
                if (!open) return;
                if (ev.key === 'Enter' || ev.key === ' ') {
                  ev.preventDefault();
                  open();
                }
              }}
            >
              <div className="rec-main">
                {editing === r.id ? (
                  <form
                    onSubmit={(ev) => {
                      ev.preventDefault();
                      setEditing('');
                      void act(r.id, () => api.rename(r.id, draft.trim()));
                    }}
                  >
                    <input
                      autoFocus
                      className="field"
                      value={draft}
                      onChange={(e) => setDraft(e.target.value)}
                    />
                    <button type="submit" className="btn btn-primary">
                      Save
                    </button>
                    <button type="button" className="btn" onClick={() => setEditing('')}>
                      Cancel
                    </button>
                  </form>
                ) : (
                  <div className="rec-title">
                    {r.title || r.id}
                    {r.live && <span className="rec-badge">● recording</span>}
                    {/* Which session to open, before opening any of them. */}
                    {r.errors !== undefined && r.errors > 0 && (
                      <span className="rec-badge">
                        {r.errors} error{r.errors === 1 ? '' : 's'}
                      </span>
                    )}
                  </div>
                )}
                <div className="dim small">
                  {new Date(r.startedAt).toLocaleString()} ·{' '}
                  {r.live ? 'running' : clock(r.durationMs)} · {r.frames} frames ·{' '}
                  {humanBytes(r.bytes)}
                  {r.hasMp4 && ' · mp4'}
                </div>
                {r.live && (
                  <div className="dim small">
                    {source(r.source)} is writing this now. It can be played once it stops.
                  </div>
                )}
                {r.partial && (
                  <div className="warn small">
                    This recording was interrupted, so it has no manifest yet.
                  </div>
                )}
              </div>

              {/* The row opens the player, so an action must not bubble up to it.
                  A live row gets no actions at all: rename and export need the
                  manifest that the stop will write, and delete would pull the
                  directory out from under a running recorder. */}
              <div className="rec-actions" onClick={(ev) => ev.stopPropagation()}>
                {r.live ? null : r.partial ? (
                  <button
                    type="button"
                    className="btn"
                    onClick={() => void act(r.id, () => api.repair(r.id))}
                  >
                    Repair
                  </button>
                ) : (
                  <>
                    <button
                      type="button"
                      className="btn"
                      onClick={() => {
                        setEditing(r.id);
                        setDraft(r.title);
                      }}
                    >
                      Rename
                    </button>
                    {r.hasMp4 ? (
                      <a className="btn" href={api.mp4URL(r.id)} download={`${r.id}.mp4`}>
                        Download
                      </a>
                    ) : (
                      <button
                        type="button"
                        className="btn"
                        onClick={() => void act(r.id, () => api.encode(r.id))}
                      >
                        Export MP4
                      </button>
                    )}
                  </>
                )}
                {!r.live && (
                  <button
                    type="button"
                    className="btn btn-danger"
                    onClick={() => {
                      if (confirm(`Delete ${r.title || r.id}?`)) {
                        void act(r.id, () => api.remove(r.id));
                      }
                    }}
                  >
                    Delete
                  </button>
                )}
              </div>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function source(name?: string): string {
  if (name === 'cli') return 'atr record';
  if (name === 'live-view') return 'The live view';
  return name || 'Another process';
}
