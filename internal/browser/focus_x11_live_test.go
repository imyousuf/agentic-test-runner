//go:build linux

package browser

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/proto"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/ewmh"
	"github.com/jezek/xgbutil/icccm"

	"github.com/imyousuf/agentic-test-runner/internal/config"
)

// The whole point of the focus work is what a person at a desktop sees, so it
// is proved on one: a headed Chrome for Testing, a real window manager, and a
// window standing in for whatever the user is working in.
//
// It needs a display nobody is using, so it runs only when given one. A nested
// X server with its own window manager does:
//
//	Xephyr :57 -screen 1280x800 -ac &
//	DISPLAY=:57 dbus-run-session -- mutter --x11 --replace --sm-disable &
//	ATR_FOCUS_DISPLAY=:57 go test -run TestTheBrowserDoesNotTakeTheFocus ./internal/browser/
func TestTheBrowserDoesNotTakeTheFocus(t *testing.T) {
	display := os.Getenv("ATR_FOCUS_DISPLAY")
	if display == "" {
		t.Skip("set ATR_FOCUS_DISPLAY to a display with a window manager to run this; see the comment")
	}
	if _, err := exec.LookPath("xlogo"); err != nil {
		t.Skip("needs xlogo to stand in for the user's window")
	}

	// Everything below — Chrome, the keeper's own connection, this test's —
	// is on that display, and the rest of the package is on none.
	t.Setenv("DISPLAY", display)

	xu, err := xgbutil.NewConn()
	if err != nil {
		t.Fatalf("connecting to %s: %v", display, err)
	}
	defer xu.Conn().Close()

	users := startUsersWindow(t, xu)
	inFront := func() bool {
		w, err := ewmh.ActiveWindowGet(xu)
		return err == nil && w == users
	}
	putUserInFront := func() {
		t.Helper()
		for i := 0; i < 20; i++ {
			_ = ewmh.ActiveWindowReq(xu, users)
			time.Sleep(100 * time.Millisecond)
			if inFront() {
				return
			}
		}
		t.Fatal("could not give the stand-in window the focus")
	}
	// settled reports whether the user's window has the focus once the
	// browser has had time to take it — longer than any grab measured.
	settled := func() bool {
		time.Sleep(700 * time.Millisecond)
		return inFront()
	}

	putUserInFront()

	b, err := New(config.BrowserConfig{
		Headless:  false,
		NoSandbox: true,
		// Chrome for Testing: the build ATR downloads, and the one that
		// focuses its window on a mouse-down.
		Executable: "auto",
		Version:    "latest",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Launch(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	if b.focus == nil {
		t.Fatal("no focus keeper on a display with a window manager")
	}

	ctx := context.Background()

	steps := []struct {
		name string
		do   func() error
	}{
		{"opening the first page", func() error { return b.NewPage(ctx, testFixtureURL+"/test_fixture.html") }},
		{"a click", func() error { return b.Click(ctx, "#test-button", false) }},
		{"another click", func() error { return b.Click(ctx, "#test-button", false) }},
		{"a double-click", func() error { return b.Click(ctx, "#test-button", true) }},
		{"a click on visible text", func() error { return b.Click(ctx, "Test Link", false) }},
		{"Tab out of the page", func() error {
			for i := 0; i < 40; i++ {
				if err := b.PressKey("Tab"); err != nil {
					return err
				}
			}
			return nil
		}},
		{"opening a second page", func() error { return b.NewPage(ctx, testFixtureURL+"/has_text.html") }},
		{"selecting the first page", func() error { return b.SelectPage(0) }},
		{"selecting the second page", func() error { return b.SelectPage(1) }},
	}
	for _, step := range steps {
		putUserInFront()
		if err := step.do(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if !settled() {
			t.Errorf("%s took the focus from the user's window", step.name)
		}
	}

	// Every page renders though none was ever brought to the front.
	for i := 0; i < 2; i++ {
		if err := b.SelectPage(i); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			data, err := b.Screenshot(false)
			if err == nil && len(data) == 0 {
				err = os.ErrInvalid
			}
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("screenshot of page %d: %v", i, err)
			}
		case <-time.After(15 * time.Second):
			t.Errorf("screenshot of page %d never returned: the page is not rendering", i)
		}
	}

	// The control. Without the keeper, activating a page does take the focus
	// on this display — so the steps above passed because the focus was kept,
	// not because nothing here could have taken it.
	putUserInFront()
	page, err := b.CurrentPage()
	if err != nil {
		t.Fatal(err)
	}
	if err := (proto.TargetActivateTarget{TargetID: page.TargetID}).Call(b.browser); err != nil {
		t.Fatal(err)
	}
	if settled() {
		t.Error("control: an unguarded activation did not take the focus, so this display cannot tell a kept focus from an untested one")
	}
}

// startUsersWindow opens a window to stand in for the user's work, and returns
// it once the window manager has it.
func startUsersWindow(t *testing.T, xu *xgbutil.XUtil) xproto.Window {
	t.Helper()

	cmd := exec.Command("xlogo", "-geometry", "300x200+40+40", "-title", "users-window")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting xlogo: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		clients, _ := ewmh.ClientListGet(xu)
		for _, w := range clients {
			if class, err := icccm.WmClassGet(xu, w); err == nil && strings.EqualFold(class.Class, "xlogo") {
				return w
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the stand-in window never appeared")
	return 0
}
