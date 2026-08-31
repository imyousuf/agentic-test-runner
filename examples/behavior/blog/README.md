# A worked example: four specs against a live blog

Everything else under `examples/behavior/` is a spec on its own. This
directory is the whole shape of a compiled suite — spec, inputs, shared
operations, and the JavaScript the compiler produced from them — because the
parts only make sense together.

```
read-a-post.test.txt          the spec, in English
read-a-post.test.properties   its inputs, committed
read-a-post.test.js           what the compiler wrote, committed
_shared.js                    operations ATR hoisted, committed
```

## These run against the internet

`base_url` points at https://imytech.net, a real Hugo blog that nobody here
controls the publishing schedule of. That is the point — it is the only
example in the repository written against a site with no `data-testid`
anywhere, whose content changes when its author publishes, and whose selectors
are whatever the theme happened to emit.

**They are deliberately not part of `make test`.** A unit suite that reaches
the public internet fails for reasons that have nothing to do with the change
under review. Run them by hand:

```bash
atr run --behavior examples/behavior/blog/ --no-compile --headless
```

`--no-compile` replays the committed scripts and costs no model calls. Drop it
and an edited spec recompiles, which needs a configured backend and drives the
live site.

## What they are examples *of*

The specs are written the way `skills/atr-author` says to write them, and the
compiled scripts show what that produces:

- **Expectations that survive publication.** No spec hardcodes a post title,
  because the newest post changes. They asserts shapes — a title exists, this
  page's first title differs from that page's — which is the "count before and
  after, never absolutely" rule.
- **A shared library nobody wrote.** `_shared.js` was not authored by hand.
  The two tag specs were compiled independently and both re-derived the same
  journey — open the tags index, wait for a tag's link, follow it — so ATR
  hoisted it into `openTagsPage` / `openTagPage` and rewrote both scripts to
  call them. It split the journey in two because
  `tag-shows-its-own-posts.test.js` counts the tags on the index *between*
  those two halves, and one four-line helper would have swallowed that.
- **What the hoist is allowed to touch.** Operations moved; not one assertion
  did. That is checked on the syntax tree before anything is written — every
  rewritten step must claim exactly what it claimed before, character for
  character — and then both rewrites were replayed against the live blog
  before being kept. `expect` is refused from a library frame at runtime, so
  the assertions stay in the specs where you can read them.
- **A spec describes a journey, not a call.** None of these specs names a
  library function. Deciding what deserves a name is ATR's job, and a spec
  that asks for `openFirstPost()` gets a local function of that name invented
  for it — which reads like sharing and is not.
- **Assertions that wait.** The compiler reached for `atr.expectText`,
  `atr.expectExists` and `atr.expectMissing` rather than a wait followed by a
  check, so a page that stops reaching a state is reported as the application
  being wrong rather than as a timeout.
- **Notes for the compiler.** Each spec ends with what the model could not
  work out by looking at one page — that the site has no test ids, and which
  values change between runs.

## When they break

If the blog is redesigned these will fail, and that is worth watching: it is
the same drift a real suite hits. A `not_found` failure means the theme moved
a selector — re-run without `--no-compile` and the agent repairs the script.
An `assertion` failure means the page genuinely stopped doing what the spec
describes.

Editing `_shared.js` does **not** force a recompile. The scripts carry a
second `atr-lib-sha256` header; when the library changes they replay and are
restamped, so a one-line fix to a shared operation costs no model calls.
