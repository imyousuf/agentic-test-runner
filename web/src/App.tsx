import { useEffect, useState } from 'react';
import { useLiveView } from './useLiveView';
import { Viewport } from './Viewport';

export function App() {
  const { state, send, takeFrame, header } = useLiveView();
  const [draft, setDraft] = useState('');
  const [hold, setHold] = useState(false);

  const active = state.pages.find((p) => p.active) ?? state.pages[0];

  useEffect(() => {
    if (active) setDraft(active.url);
  }, [active?.url]);

  const submitUrl = (ev: React.FormEvent) => {
    ev.preventDefault();
    if (draft.trim()) send({ t: 'navigate', url: draft.trim() });
  };

  const togglePolicy = () => {
    const next = !hold;
    setHold(next);
    send({ t: 'policy', foreground: next ? 'hold' : 'follow' });
  };

  return (
    <div className="app">
      <div className="tabs">
        {state.pages.map((page) => (
          <button
            key={page.id}
            className={page.active ? 'tab active' : 'tab'}
            title={page.url}
            onClick={() => send({ t: 'selectPage', id: page.id })}
          >
            {page.title || page.url || 'untitled'}
          </button>
        ))}
      </div>

      <form className="urlbar" onSubmit={submitUrl}>
        <input
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          spellCheck={false}
          placeholder="https://"
        />
        <button type="submit">Go</button>
        <button type="button" onClick={togglePolicy} className={hold ? 'on' : ''}>
          {hold ? 'Holding my tab' : 'Following the agent'}
        </button>
      </form>

      {!state.streaming && state.connected && (
        <div className="banner">
          Another tab is in front, so this page sends no frames. Switch tabs, or choose
          &ldquo;Holding my tab&rdquo;.
        </div>
      )}
      {state.error && <div className="banner error">{state.error}</div>}

      <div className="stage">
        <Viewport
          takeFrame={takeFrame}
          header={header}
          send={send}
          disabled={state.viewOnly}
        />
      </div>

      <div className="status">
        <span className={state.connected ? 'dot ok' : 'dot bad'} />
        <span>{state.connected ? 'connected' : 'disconnected'}</span>
        <span>{state.fps} fps</span>
        <span>
          {state.viewers} viewer{state.viewers === 1 ? '' : 's'}
        </span>
        {state.viewOnly && <span className="warn">view only</span>}
        <span className="grow" />
        <span>{active?.url ?? ''}</span>
      </div>
    </div>
  );
}
