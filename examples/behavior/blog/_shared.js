// Shared browser operations for the tests in this directory.
// Operations only: no assertions, no steps, nothing executed at load time.

// Open the tags index page and wait for the link to the named tag to appear.
// Leaves: the tags index page (tagsPath), with the anchor
// a[href$="/tags/<tag>"] present and visible, and the rest of the tag list
// still on screen and readable.
function openTagsPage(tagsPath, tag) {
  atr.navigate(tagsPath);
  atr.waitFor('a[href$="/tags/' + tag + '"]', { timeout: 15000, visible: true });
}

// Follow the named tag's link from the tags index page.
// Leaves: that tag's own page, with div[role="main"] .measure present and
// visible — the tag heading and its list of posts are rendered.
function openTagPage(tag) {
  atr.click('a[href$="/tags/' + tag + '"]');
  atr.waitFor('div[role="main"] .measure', { timeout: 15000, visible: true });
}
