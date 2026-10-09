# Web UI redesign — `atr remote`

Status: draft for review
Depends on: [`docs/remote-live-view.md`](./remote-live-view.md),
[`docs/session-recording.md`](./session-recording.md)

## 1. Scope

Rework the layout of the `atr remote` web application. Three views change: the
live view, the recordings library, and the player.

**In scope.** A design token layer, a light theme and a dark theme, a rebuilt
top bar, a page picker, a poster grid for the library, and a rebuilt player.
One small Go change adds a poster field to the recording summary.

**Out of scope.** The WebSocket protocol, the screencast, the recorder, and the
`atr record` CLI. No new dependency. No UI framework.

## 2. What is wrong today

I opened all three views at 2000 px and measured the faults.

| # | Fault | Where |
|---|---|---|
| 1 | The canvas sits at the top and never scales. About 60 % of the window stays black. | Live view, player |
| 2 | Nothing caps the line length. A library row spans 2000 px, so the title and its buttons sit at opposite edges. | Library, status bar |
| 3 | The library shows no picture, although frame 1 is a free poster. | Library |
| 4 | Rename, Download and Delete share one style. Nothing shows the main action. | Library |
| 5 | The scrub bar is an unstyled `input[type=range]`. The thumb is blue, thin, and hard to hit. | Player |
| 6 | The tab strip shows 8 of 27 pages, cuts them off, and gives no scroll cue. | Live view |
| 7 | The type scale is flat: 13 px body, 13 px bold title, 12 px metadata. | All |
| 8 | Five CSS blocks repeat the same six button declarations. | `styles.css` |

## 3. Design tokens

One token layer replaces the six ad-hoc variables. Plain CSS custom
properties. No Tailwind, and no build change.

```css
:root {
  color-scheme: light dark;

  --s-1: 4px;  --s-2: 8px;   --s-3: 12px;
  --s-4: 16px; --s-5: 24px;  --s-6: 32px;

  --r-1: 6px;  --r-2: 10px;  --r-3: 14px;  --r-pill: 999px;

  --t-xs: 11px; --t-sm: 12px; --t-md: 14px;
  --t-lg: 16px; --t-xl: 20px;

  --ring: 0 0 0 2px var(--accent);
}
```

The base size moves from 13 px to 14 px. A title becomes 16 px semibold, and
metadata stays 12 px. That gives three clear levels.

## 4. The two themes

The application follows the operating system. A user can override the choice.

```css
/* Light is the default set. */
:root { --bg: #f6f7f9; --surface: #fff; --text: #1a1d21; … }

/* The system asks for dark. */
@media (prefers-color-scheme: dark) {
  :root:not([data-theme='light']) { --bg: #0e0f11; … }
}

/* The user asked for one theme by hand. */
:root[data-theme='dark']  { --bg: #0e0f11; … }
:root[data-theme='light'] { --bg: #f6f7f9; … }
```

| Token | Light | Dark |
|---|---|---|
| `--bg` | `#f6f7f9` | `#0e0f11` |
| `--surface` | `#ffffff` | `#17181b` |
| `--surface-2` | `#eef0f4` | `#1f2126` |
| `--line` | `#dfe3e8` | `#2a2d33` |
| `--text` | `#1a1d21` | `#e8eaed` |
| `--dim` | `#5f6672` | `#9aa0a6` |
| `--accent` | `#0f7b7b` | `#4ec9c9` |
| `--danger` | `#c2352f` | `#ff6b6b` |

`useTheme.ts` holds `'auto' | 'light' | 'dark'`, writes `data-theme` on the
root element, and saves the choice in `localStorage`. The default is `auto`.

**One rule the themes must not break.** The canvas shows a screenshot of some
other page. A white page on a white background loses its edge. The canvas keeps
a neutral mat, a 1 px border and a soft shadow in both themes.

## 5. One button system

`.btn` carries the shared shape. Four modifiers give the rank.

| Class | Use |
|---|---|
| `.btn-primary` | Filled with the accent. One per view. Play, and Go. |
| `.btn` | The quiet default. Most controls. |
| `.btn-danger` | Delete. |
| `.btn-icon` | A square icon button, 32 px. |
| `.seg` | A segmented control. The player speeds use it. |

This removes the five duplicate blocks listed in §2, fault 8.

## 6. The live view

### 6.1 One bar instead of two

The tab strip and the URL bar merge into one bar.

```
┌────────────────────────────────────────────────────────────────────┐
│ [▾ Team Messages | Opal  27]   [ https://opal…/messages    ] [Go]  │
│                                  ⇄ Follow  ● Record  ▤ Recordings ◐│
└────────────────────────────────────────────────────────────────────┘
```

The left control is the page picker. The URL field is a pill, and it caps at
720 px so the text stays near the middle. The right group holds the foreground
toggle, the record button, the library link, and the theme button.

The record button is always on the bar. It used to render nothing when the
server had no recordings directory, and nothing again on a view-only link, so
the feature looked missing rather than unavailable. It is now disabled instead,
and the reason is on the button. Beside it, `● atr record is recording · 0:45`
appears when another process is recording this browser.

### 6.2 The page picker

The tab strip fails at 27 pages. A picker does not.

- The button shows the active title and the page count.
- A click opens a panel with a filter field and the full list.
- Each row shows the title, the host, and a dot when the page is active.
- Type to filter. Use the arrow keys to move. Press Enter to select.
- Press Escape to close.

### 6.3 Fill the stage

`.stage` centres the canvas on both axes. The canvas scales to fit the free
space, and it keeps its aspect ratio.

`max-height` alone cannot do this. It clamps the height and leaves the width
alone, so the picture stretches. The stage becomes a size container instead,
and the canvas takes the smaller of the two limits:

```css
.stage.fit { container-type: size; }
.stage.fit .viewport { width: min(100cqw, 100cqh * var(--ar, 1.6)); height: auto; }
```

`FrameCanvas` writes `--ar` when the frame size changes. The box stays exactly
the size of the drawn image, so `Viewport.toPage` keeps working.

A 1280 px frame on a 2000 px stage scales up 1.5 times, and a JPEG looks a
little soft at that size. So the status bar carries a **Fit / 1:1** toggle. Fit
is the default.

### 6.4 The status bar

The items group at the left as pills. The URL stays at the right, and it
truncates from the middle.

```
● connected   18 fps   2 viewers   ● recording        opal…/messages  Fit 1:1
```

## 7. The library

### 7.1 A poster grid

```
  Recordings                            [ Search ]  [ Newest ▾ ]  [ ↻ ]
  ─────────────────────────────────────────────────────────────────────
  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐
  │▒▒ frame 1 ▒│ │▒▒ frame 1 ▒│ │▒▒ frame 1 ▒│ │▒▒ frame 1 ▒│
  │       0:05 │ │       1:42 │ │  0:31  mp4 │ │    partial │
  └────────────┘ └────────────┘ └────────────┘ └────────────┘
   Smoke test     Checkout        Login flow     Interrupted
   12:36 · 221 KB 09:14 · 44 MB   Aug 29 · 8 MB  Aug 28 · 2 MB
                ⋯              ⋯              ⋯              ⋯
```

- The grid is `repeat(auto-fill, minmax(260px, 1fr))`, and it caps at 1200 px.
- The poster crops to 16:9 with `object-fit: cover`.
- The duration sits on the poster. So do the `mp4` and `partial` badges.
- The whole card opens the player. This keeps the behaviour you asked for.
- A `⋯` menu holds Rename, Export MP4 or Download, Repair, and Delete.
- Search filters the title and the id. Sort offers newest, longest, largest.
- The empty state explains how to make the first recording.
- A recording that is still being written shows a **● recording** badge, says
  which process is writing it, and carries no actions. Rename and Export need
  the manifest the stop will write, and Delete would pull the directory out
  from under a running recorder.

### 7.2 Where the poster comes from

`Summary` carries no frame name today, and the first file is **not** always
`000001.jpg`. With `--keep-last` the ring drops the early frames, so the first
surviving file can be `000042.jpg`.

So `record.Summary` gains one field:

```go
Poster string `json:"poster,omitempty"` // the first frame that survived
```

`Store.List` fills it from `Manifest.Frames[0].File`. A partial recording has
no manifest, so it has no poster, and the card shows a placeholder. The
existing route serves the image, and no new route is needed.

This is the only Go change in the redesign.

## 8. The player

### 8.1 Layout

```
┌────────────────────────────────────────────────────────────────────┐
│ ← Recordings   Smoke test          3 frames · 221 KB · Chrome/152  │
├────────────────────────────────────────────────────────────────────┤
│                                                                    │
│                     ┌──────────────────────┐                       │
│                     │                      │                       │
│                     │     the frame        │                       │
│                     │                      │                       │
│                     └──────────────────────┘                       │
│                                                                    │
├────────────────────────────────────────────────────────────────────┤
│ ▌·     ▬                  ▌              ·        the mark row     │
│ ━━━━━━━━━━━━━━●─────────────────────────────────────────────────── │
│ ◀| ▶ |▶ [◀|│|▶] 0:02 / 0:05 [0.5×│1×│2×│4×]  ☐ Skip gaps   ⬇ MP4   │
└────────────────────────────────────────────────────────────────────┘
```

### 8.2 The scrub bar

**Built, and it is not a range input.** I had planned to style
`input[type=range]`, for the keyboard and screen reader behaviour it gives
free. A range input cannot hold the activity graph: it has one thumb, one
track, and no place to put a clickable region inside it. `Scrubber.tsx` is a
`div` with `role="slider"` and the aria value attributes instead.

```
▁▃█▅▂▁ ░░0:14░░ ▂█▇█▅▃▁▂ ░░0:12░░ ▁▄█▆▂ ░░0:11░░
```

- Each column is the change score for that moment, on a log scale, taken as
  the loudest frame in the column. The scores span three decades, so a linear
  bar would be a flat line with a few spikes.
- A hatched block is a stretch where nothing changed, labelled with how long
  it really lasted. Click it to play it in full.
- The played part is a 14% accent tint over the whole height, not a fill. A
  solid fill would hide the half of the session already watched.
- **A 16 px mark row sits along the top edge**, above the bars and separated by
  a rule. It was drawn as ticks along the bottom at first. They were 2 px wide,
  they sat under the bars, and nobody found them. The marks are what a person
  looks for — the click, the navigation — so they get their own row and a 16 px
  hit target around a 6 px pip.
- A pip takes a shape from its kind: a tall accent bar for `nav` and `tab`, a
  text-coloured dot for `click`, a text-coloured dash for `type`, a warn dot for
  `key` and `stall`, an ok dot for `resume`. Shape first and colour second, so
  the row survives a colour-blind reader and a dark theme.
- Marks within 1/140 of the bar merge into one, and the cluster takes the shape
  of its most notable member. The tooltip names every event in it.
- Everything inside is positioned by played time, not recording time, because
  a cut stretch is narrower on the bar than it really was.

A label is drawn only when its block is at least 3% of the bar. A clipped
label reads as a wrong number.

### 8.3 The controls

The transport groups at the left. Play is the one primary button. The speed
buttons become a segmented control. Skip inactivity and the export move to the
right.

A segmented pair sits after the frame steps: previous mark and next mark. The
up and down arrow keys do the same, and the left and right arrows keep stepping
one frame. The mark row is only useful if you can walk it without aiming a
mouse at a 6 px pip. A recording with no marks gets no pair, because two dead
controls say less than none at all.

At the end the primary button becomes **Replay** and restarts from the top. A
Play button that is already at the end has nothing to play, and pressing it and
getting nothing reads as a broken player. The space bar follows the same rule.

**Skip inactivity is on by default.** A recording is mostly waiting, so the
useful default is to cut the waiting. The checkbox says how much real time the
cuts remove, and the clock shows the played length with the real length after
it: `0:14 / 0:27 (0:49)`.

Nothing is thrown away to make this work. A cut stretch keeps its place on the
bar and opens on a click, so the viewer can always check what was skipped.

While the recording plays, the head bar and the control bar fade out after
2.5 s. Any mouse move, any key press, or a pause brings them back.

### 8.4 Icons are drawn, not typed

The controls used to be Unicode: ⏮ U+23EE, ⏭ U+23ED and ⏸ U+23F8. The
stylesheet asks for `system-ui` and loads no icon font, and `system-ui` on Linux
has no glyph for any of the three, so they drew as empty boxes. ▶ survived,
which is why Play looked right and the step buttons did not.

`Icon.tsx` holds one inline SVG per name: play, pause, replay, stepBack,
stepForward, prevMark, nextMark, record, stop, auto, light, dark. Each sizes at
`1em` and paints with `currentColor`, so it follows the button it sits in, on
every platform. The transport glyphs paint a filled body, because an outline is
mud at 14 px. No icon font, and no new dependency.

The page had no favicon at all, so every browser asked for `/favicon.ico` and
got a 404 in the log. `web/public/favicon.svg` is an accent tile with a tab
strip over a record dot — the live view, and what it is for. One `<link
rel="icon" type="image/svg+xml">` in `index.html` serves it, and Vite copies
`web/public/` into the embedded `dist`.

## 9. Accessibility

- One focus ring token, applied through `:focus-visible` everywhere.
- The page picker and the `⋯` menu trap the arrow keys, and Escape closes them.
- `prefers-reduced-motion` stops the record pulse, the card lift, and the
  chrome fade.
- Every icon-only button carries an `aria-label`.

## 10. Files

**New**

```
web/src/useTheme.ts        auto, light or dark; saved in localStorage
web/src/ThemeButton.tsx    the switch
web/src/Menu.tsx           a popover menu; the cards use it
web/src/AppBar.tsx         the merged top bar
web/src/PagePicker.tsx     the replacement for the tab strip
web/src/RecordingCard.tsx  one card in the grid
web/src/Scrubber.tsx       the activity bar, the mark row and the idle blocks
web/src/activity.ts        scores to spans, marks, and the played timeline
web/src/Icon.tsx           one inline SVG per control
web/public/favicon.svg     the tab icon
```

**Changed**

```
web/src/styles.css         rewritten around the tokens
web/src/App.tsx            routes and the new bar
web/src/Library.tsx        the grid, the search and the sort
web/src/Player.tsx         the layout, the scrubber and the controls
web/src/Viewport.tsx       fit and 1:1
web/src/RecordButton.tsx   the new button classes; disabled, never hidden
web/src/api.ts             posterURL
web/src/protocol.ts        Summary.poster, Summary.live, RecordMsg.elsewhere
web/index.html             the favicon link
internal/record/types.go   Summary.Poster
internal/record/store.go   fill it in List
internal/record/store_test.go
```

## 11. Phases

| Phase | Work | Result |
|---|---|---|
| 1 | Tokens, the two themes, the button system, the stage fill — **done** | The largest visual gain, and the smallest risk |
| 2 | AppBar, PagePicker, the status bar | The 27-tab problem goes away |
| 3 | `Summary.Poster`, Menu, the poster grid, search and sort | The library becomes readable |
| 4 | Scrubber, the player controls, the chrome fade | Playback feels finished |
| 5 | Reduced motion, empty states, focus rings, narrow screens | Polish |

Each phase builds and ships on its own.

## 12. Test plan

- `tsc -b` type-checks every phase.
- `go test ./internal/record/` covers the new `Poster` field, and it must stay
  empty for a partial recording.
- A screenshot of each of the three views, in the light theme and in the dark
  theme, at 1280 px and at 2000 px.
- Keyboard only: reach the page picker, pick a page, open the library, open a
  card, play, scrub, and return.
- A recording made with `--keep-last` must show a poster that is not
  `000001.jpg`.

## 13. Risks

| Risk | Answer |
|---|---|
| An upscaled JPEG looks soft. | The Fit / 1:1 toggle. Fit is the default. |
| A light theme behind a dark screenshot looks wrong. | The canvas keeps a neutral mat in both themes. |
| The chrome fade hides the controls when a user wants them. | Any move brings them back, and a pause pins them. |
| `web/dist` is committed, so every phase re-commits the bundle. | Expected. One bundle per phase commit. |
| The grid loads one image per card. | The frames are small, and the route already sends an immutable cache header. |

## 14. Confidence

**86 out of 100.**

Phases 1, 3 and 5 are routine, and I rate them 95. Phase 4 is the scrub bar,
and cross-browser range styling always costs an extra pass; I rate it 85. Phase
2 is the page picker, which is new interaction rather than new styling, and I
rate it 80.
