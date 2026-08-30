// Shared operations for the blog specs.
//
// Operations only: no assertions here, and nothing runs at load time.

// Opens the front page and waits until the post list is on screen.
// Leaves the browser on the home page.
function openHome() {
  atr.navigate("/");
  atr.waitFor("a.post-link");
}

// Follows the newest post's link and waits for the article to load.
// Leaves the browser on that post's page.
//
// The whole card is wrapped in the anchor — <a class=post-link> contains the
// date, the h3 title and the summary — so the link is a.post-link and not
// anything nested under a heading.
function openFirstPost() {
  openHome();
  atr.click("a.post-link");
  atr.waitFor("h1");
}
