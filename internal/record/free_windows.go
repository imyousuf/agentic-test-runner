package record

// checkFree is a warning only on Windows. Reading the free space needs a
// syscall this build does not link, and a missing number must not stop a
// recording that would otherwise work.
func checkFree(_ string) Check {
	return Check{
		Name:   "free disk space",
		Warn:   true,
		Detail: "not checked on Windows",
	}
}
