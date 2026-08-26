# In-Page Agent HUD

The HUD is a floating panel injected into the browser window that lets you hand
work to the ATR agent without leaving the page you are looking at. You type a
request; the agent carries it out on the page in front of you.

It is meant for a **headed** browser — a visible window you are actively using.
The panel needs somewhere to render, so it does nothing in headless mode.

```bash
atr browser start --hud            # start headed with the panel showing
# or, against a daemon that is already running:
atr browser hud on
atr browser hud status
atr browser hud off
```

## What it can do

The HUD agent is not limited to the browser. It has:

| Tool | Purpose |
| --- | --- |
| `browser_*` | Drive the page: snapshot, click, fill, navigate, scroll, screenshot, evaluate, and the rest of the browser toolset |
| `browser_fill_secret` | Fill a credential without ever seeing its value (below) |
| `execute_command` | Run a shell command |
| `read_file` | Read a file from disk |
| `search_code` | Search the codebase |

Shell, file and search tools operate in the directory you ran `atr browser hud
on` from. Override it with `--working-dir`.

Some things it is good for:

```
Log me in as the test user and stop at the dashboard.
This form rejects my input — what validation is it running? Check the JS bundle.
Fill the password field from keyring github/password.
Compare this page against the mock in ./designs/checkout.png.
Why is the submit button disabled? Read the component source.
```

## Passwords

Ask the agent to fill a credential and it never sees the value. You give it the
command; ATR runs the command itself and types the output straight into the
field:

```
fill the password field by running: pass show github/password
```

The agent gets back only `Filled the secret from the supplied command into
"#password" (18 characters). The value was not disclosed.`

### Why not just use the shell tool?

Because it leaks. If the agent runs `pass show github/password` through
`execute_command` and passes the result to `browser_fill`, the plaintext
becomes a tool result. Tool results join the message history, and the history
is re-transmitted to your model provider on **every subsequent turn** of the
conversation. One password fill would put your password in every request for
the rest of the session.

`browser_fill_secret` fetches and consumes the secret inside a single tool
call. The value is never a tool result, so there is nothing to leak.

The system prompt instructs the agent to always use `browser_fill_secret` for
credentials. As a backstop, any value ATR has fetched is scrubbed out of later
tool results — so a page that echoes the password back does not leak it either.

### Naming secrets instead of commands

Put the commands in `~/.atr/config.yaml` and refer to them by name:

```yaml
secrets:
  timeout: "60s"        # password managers may block on a biometric prompt
  refs:
    github/password: "secret-tool lookup service atr account github/password"
    work/vpn: "pass show work/vpn"
    aws/key: "op read op://Private/aws/credential"
```

Then just ask for `the github/password secret`. The panel shows which field was
filled, never the ref or the command — a command can embed an entry path you
would rather not display on a shared screen.

Refs are worth setting up for a second reason: **what you type into the panel
is a user message, and user messages go to the model.** The protection covers
the secret's *value*, not the text of your prompt. Asking for a ref by name
sends only the name. Typing a command sends the command — fine for
`pass show github/password`, not fine if you paste the credential itself.

Any manager that prints to stdout works: `pass`, `secret-tool` (libsecret),
`security` (macOS Keychain), `op` (1Password), `bw` (Bitwarden), `gopass`, or a
script of your own.

## How it works

The panel is injected into a named **isolated world** rather than the page's
main JavaScript context, and talks to ATR over a CDP binding rather than the
network. Two reasons:

1. **Content Security Policy.** A panel that reached the daemon over `fetch()`
   or a WebSocket would be blocked outright by `connect-src` on any site with a
   strict policy. A CDP binding is not a network request, so no CSP directive
   applies to it. The HUD works on GitHub, on your bank, anywhere.
2. **Isolation.** Globals in an isolated world are invisible to page scripts,
   so a page cannot detect, call, or tamper with the bridge.

Isolated worlds still share the DOM, so the panel is a normal element hosting a
**closed shadow root**. That is what keeps the HUD out of the agent's own view
of the page: `atr browser snapshot` walks the DOM with `querySelectorAll`,
which does not pierce shadow boundaries. Screenshots hide the panel for the
duration of the capture.

Chrome recreates the isolated world on every navigation and re-runs the
injected script, so the panel comes back by itself after a page load, and new
tabs get their own. The conversation belongs to the session, not to any page —
navigate away and the transcript is still there.

## Security

The HUD agent can run shell commands, which means **page content it reads is
untrusted input to something that can act on your machine**. The system prompt
tells it to treat page text as data and to report rather than obey any
instructions it finds there, but prompt injection is not a solved problem.

Practical guidance:

- Turn the HUD on for the browsing you want help with, and off afterwards
  (`atr browser hud off`). It is not meant to be left on all day.
- Prefer `--working-dir` pointing at the project you are working on rather than
  a home directory.
- Watch the tool rows in the panel. Every shell command the agent runs is shown
  there before its result comes back.
- The daemon binds to localhost, but anything that can reach the daemon's port
  can enable the HUD. Do not expose the browser daemon's port off the machine.

## Limitations

- Headed browsers only. `--hud` with `--headless` warns and does nothing.
- Not exposed over MCP. An agent enabling an in-page panel for itself is not a
  useful operation; the HUD exists for a human at the keyboard. Agents drive
  ATR through the existing `browser_*` tools.
- One request at a time. Sending while a request is running cancels it.
- The panel renders plain text. Agent replies are prose, not markdown.
