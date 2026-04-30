package computer

// activeAppID returns the platform-specific identifier of the currently
// focused window's owning application. Used as the per-app cache key.
//
// On Linux it returns WM_CLASS (e.g. "Google-chrome", "Gnome-terminal"),
// which is stable across that app's windows. On macOS / Windows v1
// returns the active window title; future work will use bundle id /
// process exe.
//
// Returns "" if it can't be determined; callers treat that as "always
// prompt" rather than caching a blank entry.
func (c *Computer) activeAppID() string {
	id, _ := platformActiveAppID()
	return id
}
