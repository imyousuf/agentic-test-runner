// ATR in-page agent HUD.
//
// Runs in a named isolated world, so nothing here is reachable from page
// script and no Content Security Policy applies to it. The panel's markup
// lives behind a closed shadow root: page CSS cannot style it, page script
// cannot read it, and the agent's own Snapshot() -- which uses
// querySelectorAll -- does not see it either.
(() => {
  'use strict';

  // Only the top-level document gets a panel. Without this every iframe on
  // the page would mount its own copy.
  if (window.top !== window) return;
  if (globalThis.__atrHudMounted) return;
  globalThis.__atrHudMounted = true;

  const HOST_ID = '__atr_hud_host';
  const send = (msg) => {
    try {
      globalThis.__atrHudSend(JSON.stringify(msg));
    } catch (_) {
      // The binding is absent if the HUD was disabled mid-session. Nothing
      // useful to do; the panel simply stops talking.
    }
  };

  const CSS = `
    :host { all: initial; }
    * { box-sizing: border-box; font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif; }
    .panel {
      position: fixed; right: 16px; bottom: 16px; width: 380px; height: 460px;
      display: flex; flex-direction: column;
      background: #14161a; color: #e6e8eb;
      border: 1px solid #2c3038; border-radius: 10px;
      box-shadow: 0 12px 40px rgba(0,0,0,.45);
      font-size: 13px; line-height: 1.5; overflow: hidden;
      z-index: 2147483647;
    }
    .panel.collapsed { height: 38px; }
    .panel.collapsed .body, .panel.collapsed .composer { display: none; }
    header {
      display: flex; align-items: center; gap: 8px;
      padding: 0 10px; height: 38px; flex: 0 0 38px;
      background: #1b1e24; border-bottom: 1px solid #2c3038;
      cursor: move; user-select: none;
    }
    .dot { width: 7px; height: 7px; border-radius: 50%; background: #3fb950; flex: 0 0 auto; }
    .dot.busy { background: #d29922; animation: pulse 1s ease-in-out infinite; }
    @keyframes pulse { 50% { opacity: .25; } }
    .title { font-weight: 600; font-size: 12px; letter-spacing: .02em; flex: 1; }
    header button {
      background: none; border: none; color: #8b949e; cursor: pointer;
      font-size: 15px; line-height: 1; padding: 4px 6px; border-radius: 4px;
    }
    header button:hover { background: #2c3038; color: #e6e8eb; }
    .body { flex: 1; overflow-y: auto; overflow-x: hidden; padding: 10px; display: flex; flex-direction: column; gap: 8px; }
    .msg { padding: 7px 9px; border-radius: 7px; white-space: pre-wrap; word-break: break-word; }
    .msg.user { background: #1f6feb; color: #fff; align-self: flex-end; max-width: 85%; }
    .msg.done { background: #21262d; align-self: flex-start; max-width: 92%; }
    .msg.delta { background: #21262d; align-self: flex-start; max-width: 92%; }
    .msg.error { background: #3d1d1d; color: #ffa198; border: 1px solid #6e2b2b; }
    .msg.status { display: none; }
    .tool {
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 11px;
      color: #8b949e; padding: 3px 9px; border-left: 2px solid #30363d;
      /* Tool details are arbitrary strings — selectors, scripts, URLs. Wrap
         them; a long one must not make the whole panel scroll sideways. */
      overflow-wrap: anywhere; white-space: pre-wrap;
    }
    .tool b { color: #a5d6ff; font-weight: 600; }
    .empty { color: #6e7681; font-size: 12px; text-align: center; margin: auto; padding: 0 20px; }
    .composer { flex: 0 0 auto; border-top: 1px solid #2c3038; padding: 8px; display: flex; gap: 6px; }
    textarea {
      flex: 1; resize: none; height: 54px; padding: 6px 8px; font-size: 12.5px;
      background: #0d1117; color: #e6e8eb; border: 1px solid #30363d; border-radius: 6px;
      outline: none; font-family: inherit;
    }
    textarea:focus { border-color: #1f6feb; }
    .composer button {
      align-self: stretch; padding: 0 12px; border: none; border-radius: 6px;
      background: #238636; color: #fff; font-weight: 600; font-size: 12px; cursor: pointer;
    }
    .composer button:hover { background: #2ea043; }
    .composer button.cancel { background: #6e2b2b; }
    .composer button.cancel:hover { background: #8b3232; }
  `;

  let shadow, panel, body, input, sendBtn, dot;
  let busy = false;
  let greeted = false;

  function mount() {
    if (document.getElementById(HOST_ID)) return;

    const host = document.createElement('div');
    host.id = HOST_ID;
    // The host carries no role, aria-label or data-testid on purpose: those
    // are the attributes Snapshot() selects on, and the agent should not see
    // its own control panel as page content.
    host.setAttribute('style', 'all: initial; position: static;');
    shadow = host.attachShadow({ mode: 'closed' });
    // Kept on the isolated world's globals, never on the page's: this is how
    // the test suite inspects the panel without the shadow root being open to
    // page script.
    globalThis.__atrHudTestRoot = shadow;

    const style = document.createElement('style');
    style.textContent = CSS;
    shadow.appendChild(style);

    panel = document.createElement('div');
    panel.className = 'panel';
    panel.innerHTML = `
      <header>
        <span class="dot"></span>
        <span class="title">atr agent</span>
        <button data-act="collapse" title="Collapse">&#8211;</button>
      </header>
      <div class="body"><div class="empty">Ask the agent to do something on this page.\nIt can drive the browser, run shell commands and read files.</div></div>
      <div class="composer">
        <textarea placeholder="Ask the agent to do something…"></textarea>
        <button data-act="send">Send</button>
      </div>`;
    shadow.appendChild(panel);
    (document.body || document.documentElement).appendChild(host);

    body = panel.querySelector('.body');
    input = panel.querySelector('textarea');
    sendBtn = panel.querySelector('[data-act="send"]');
    dot = panel.querySelector('.dot');

    panel.querySelector('[data-act="collapse"]').addEventListener('click', () => {
      panel.classList.toggle('collapsed');
    });
    sendBtn.addEventListener('click', submit);
    input.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' && !ev.shiftKey) {
        ev.preventDefault();
        submit();
      }
      // Keystrokes typed into the HUD must never reach the page underneath.
      ev.stopPropagation();
    });
    makeDraggable(panel, panel.querySelector('header'));

    greet();
  }

  // The opening handshake both announces the panel and tells Go which
  // execution context to push events into. If it is lost -- the page mounted
  // in the gap between the binding being registered and the event
  // subscription going live, say -- the panel would be mounted but
  // unreachable. Retry until Go answers with the transcript.
  function greet(attempt = 0) {
    if (greeted || attempt > 20) return;
    send({ op: 'hello' });
    setTimeout(() => greet(attempt + 1), 250);
  }

  function submit() {
    if (busy) {
      send({ op: 'cancel' });
      return;
    }
    const text = input.value.trim();
    if (!text) return;
    input.value = '';
    send({ op: 'ask', text });
  }

  function setBusy(next) {
    busy = next;
    if (!dot) return;
    dot.classList.toggle('busy', busy);
    sendBtn.textContent = busy ? 'Stop' : 'Send';
    sendBtn.classList.toggle('cancel', busy);
  }

  function render(ev) {
    if (!body) return;
    const empty = body.querySelector('.empty');
    if (empty) empty.remove();

    let el;
    if (ev.t === 'tool') {
      el = document.createElement('div');
      el.className = 'tool';
      const name = document.createElement('b');
      name.textContent = ev.name || 'tool';
      el.appendChild(name);
      if (ev.detail) el.appendChild(document.createTextNode(' ' + ev.detail));
    } else {
      if (!ev.text) return;
      el = document.createElement('div');
      el.className = 'msg ' + ev.t;
      // textContent, never innerHTML: agent output is untrusted text and must
      // not be able to inject markup into the panel.
      el.textContent = ev.text;
    }
    body.appendChild(el);
    body.scrollTop = body.scrollHeight;
  }

  function makeDraggable(el, handle) {
    let startX = 0, startY = 0, originX = 0, originY = 0, dragging = false;
    handle.addEventListener('mousedown', (ev) => {
      if (ev.target.tagName === 'BUTTON') return;
      const rect = el.getBoundingClientRect();
      dragging = true;
      startX = ev.clientX; startY = ev.clientY;
      originX = rect.left; originY = rect.top;
      // Pin to left/top so the drag math has a single origin; the CSS default
      // anchors to right/bottom, which would invert the deltas.
      el.style.left = originX + 'px';
      el.style.top = originY + 'px';
      el.style.right = 'auto';
      el.style.bottom = 'auto';
      ev.preventDefault();
    });
    window.addEventListener('mousemove', (ev) => {
      if (!dragging) return;
      const w = el.offsetWidth, h = el.offsetHeight;
      // Clamp so the panel can never be dragged fully off-screen and
      // stranded.
      const x = Math.min(Math.max(0, originX + ev.clientX - startX), window.innerWidth - w);
      const y = Math.min(Math.max(0, originY + ev.clientY - startY), window.innerHeight - h);
      el.style.left = x + 'px';
      el.style.top = y + 'px';
    });
    window.addEventListener('mouseup', () => { dragging = false; });
  }

  globalThis.__atrHudDeliver = (ev) => {
    try {
      if (!panel) return;
      greeted = true;
      if (ev.t === 'state') {
        body.innerHTML = '';
        (ev.history || []).forEach(render);
        if (!body.children.length) {
          const d = document.createElement('div');
          d.className = 'empty';
          d.textContent = 'Ask the agent to do something on this page.';
          body.appendChild(d);
        }
        setBusy(!!ev.busy);
        return;
      }
      if (ev.t === 'status') {
        setBusy(!!ev.busy);
        return;
      }
      render(ev);
    } catch (_) {
      // A rendering fault must not break the bridge.
    }
  };

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', mount, { once: true });
  } else {
    mount();
  }
})();
