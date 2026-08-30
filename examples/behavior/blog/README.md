# A worked example: three specs against a live blog

Everything else under `examples/behavior/` is a spec on its own. This
directory is the whole shape of a compiled suite — spec, inputs, shared
operations, and the JavaScript the compiler produced from them — because the
parts only make sense together.

```
read-a-post.test.txt          the spec, in English
read-a-post.test.properties   its inputs, committed
read-a-post.test.js           what the compiler wrote, committed
_shared.js                    operations both specs use
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
- **A shared library of operations, not assertions.** `_shared.js` holds
  `openHome` and `openFirstPost`. `expect` is refused from a library frame, so
  the assertions stay in the specs where you can read them.
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
