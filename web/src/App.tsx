import { useEffect, useMemo, useState } from 'react';
import { DevTools } from './DevTools';
import { Icon } from './Icon';
import { Library } from './Library';
import { Player } from './Player';
import { RecordButton } from './RecordButton';
import { RecordOverlay } from './RecordOverlay';
import { TabIcon } from './TabIcon';
import { ThemeButton } from './ThemeButton';
import { useLiveView } from './useLiveView';
import { useRoute } from './useRoute';
import { Viewport } from './Viewport';

export function App() {
  const { state, send, takeFrame, header, setRecord, clearError } = useLiveView();
  const [route, go] = useRoute();
  const [draft, setDraft] = useState('');
  const [hold, setHold] = useState(false);
  const [notice, setNotice] = useState('');
  const [fit, setFit] = useState(true);
  const [dock, setDock] = useState(false);

  const active = state.pages.find((p) => p.active) ?? state.pages[0];

  // The badge counts what the page reported, not what the dock draws. It is on
  // the button so a closed dock can still say something went wrong.
  const failures = useMemo(
    () => state.log.filter((ev) => ev.t === 'error' || ev.t === 'netfail').length,
    [state.log],
  );

  useEffect(() => {
    if (active) setDraft(active.url);
  }, [active?.url]);

  if (route.view === 'player') {
    return <Player id={route.id} onBack={() => go('/recordings')} />;
  }
  if (route.view === 'library') {
    return (
      <div className="app">
        <div className="bar">
          <button type="button" className="btn" onClick={() => go('/')}>
            ← Live view
          </button>
          <span className="grow" />
          <ThemeButton />
        </div>
        <Library onOpen={(id) => go(`/recordings/${id}`)} />
      </div>
    );
  }

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
      {/* The tab is a span holding two buttons, not a button holding one. A
          button inside a button is invalid, and the browser resolves it by
          dropping the nesting, so the close would pick the tab instead. */}
      <div className="tabs">
        {state.pages.map((page) => {
          const name = page.title || page.url || 'untitled';
          return (
            <span key={page.id} className={page.active ? 'tab active' : 'tab'} title={page.url}>
              <button
                type="button"
                className="tab-pick"
                onClick={() => send({ t: 'selectPage', id: page.id })}
              >
                <TabIcon url={page.url} />
                <span className="tab-label">{name}</span>
              </button>
              {/* Closing the only tab closes the browser, so it is not offered.
                  The server refuses it as well; this just avoids a dead ✕. */}
              {state.pages.length > 1 && !state.viewOnly && (
                <button
                  type="button"
                  className="tab-close"
                  title={`Close ${name}`}
                  aria-label={`Close ${name}`}
                  onClick={() => send({ t: 'closePage', id: page.id })}
                >
                  ✕
                </button>
              )}
            </span>
          );
        })}
        {/* Last in the strip, where a browser puts it. It opens about:blank and
            the stream follows, so the URL box is the next thing to use. */}
        {!state.viewOnly && (
          <button
            type="button"
            className="tab-new"
            title="New tab"
            aria-label="New tab"
            onClick={() => send({ t: 'newPage', url: '' })}
          >
            +
          </button>
        )}
      </div>

      {/* The bar is a div and the URL box is the only form in it. RecordButton
          renders a form of its own to name a recording, and a form inside a
          form submits both: naming a recording used to navigate the browser to
          whatever was in the URL box. */}
      <div className="bar">
        <form className="bar-url" onSubmit={submitUrl}>
          <input
            className="field url"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            spellCheck={false}
            placeholder="https://"
          />
          <button type="submit" className="btn btn-primary">
            Go
          </button>
        </form>
        <button type="button" onClick={togglePolicy} className={hold ? 'btn on' : 'btn'}>
          {hold ? 'Holding my tab' : 'Following the agent'}
        </button>
        <RecordButton
          state={state.record}
          canRecord={state.canRecord && !state.viewOnly}
          reason={
            !state.canRecord
              ? 'This server was started without a recordings directory.'
              : 'This link is view only, so it cannot start a recording.'
          }
          onChange={setRecord}
          onError={setNotice}
        />
        <button
          type="button"
          className={dock ? 'btn on' : 'btn'}
          title="Console, network and issues"
          aria-pressed={dock}
          onClick={() => setDock((d) => !d)}
        >
          <Icon name="console" />
          DevTools
          {failures > 0 && <span className="pill bad">{failures}</span>}
        </button>
        <button type="button" className="btn" onClick={() => go('/recordings')}>
          Recordings
        </button>
        <ThemeButton />
      </div>

      {!state.streaming && state.connected && (
        <div className="banner">
          Another tab is in front, so this page sends no frames. Switch tabs, or choose
          &ldquo;Holding my tab&rdquo;.
        </div>
      )}
      {state.record.note && <div className="banner">{state.record.note}</div>}
      {notice && (
        <div className="banner error" onClick={() => setNotice('')}>
          {notice}
        </div>
      )}
      {state.error && (
        <div className="banner error" onClick={clearError}>
          {state.error}
        </div>
      )}

      <div className="work">
        {/* The wrapper exists so the overlay can be placed over the picture and
            only the picture. Anchored to .work it would drift as the dock
            opens, and inside .stage it would scroll away at 1:1. */}
        <div className="stage-wrap">
          <div className={fit ? 'stage fit' : 'stage actual'}>
            <Viewport
              takeFrame={takeFrame}
              header={header}
              send={send}
              disabled={state.viewOnly}
            />
          </div>
          <RecordOverlay state={state.record} onChange={setRecord} onError={setNotice} />
        </div>
        {/* No playhead and nowhere to seek: on a live page the newest row is
            the page on the screen. */}
        {dock && <DevTools rows={state.log} onClose={() => setDock(false)} />}
      </div>

      <div className="status">
        <span className="pill">
          <span className={state.connected ? 'dot ok' : 'dot bad'} />
          {state.connected ? 'connected' : 'disconnected'}
        </span>
        <span className="pill num">{state.fps} fps</span>
        <span className="pill num">
          {state.viewers} viewer{state.viewers === 1 ? '' : 's'}
        </span>
        {state.viewOnly && <span className="pill warn">view only</span>}
        {/* Recording is reported on the picture, not here. */}
        <span className="grow url-now" title={active?.url ?? ''}>
          {active?.url ?? ''}
        </span>

        {/* Fit scales a 1280 px frame up to a 2000 px window, and an upscaled
            JPEG looks soft. 1:1 is the way back. */}
        <span className="seg">
          <button type="button" className={fit ? 'on' : ''} onClick={() => setFit(true)}>
            Fit
          </button>
          <button type="button" className={fit ? '' : 'on'} onClick={() => setFit(false)}>
            1:1
          </button>
        </span>
      </div>
    </div>
  );
}
