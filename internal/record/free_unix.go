//go:build !windows

package record

import (
	"fmt"
	"syscall"
)

func checkFree(root string) Check {
	c := Check{Name: "free disk space"}
	var st syscall.Statfs_t
	if err := syscall.Statfs(root, &st); err != nil {
		c.Warn = true
		c.Detail = "could not read the free space"
		return c
	}
	free := int64(st.Bavail) * int64(st.Bsize)
	c.Detail = fmt.Sprintf("%s free", humanBytes(free))
	if free < MinFreeBytes {
		c.Err = fmt.Errorf(
			"only %s is free on %s, and a recording needs at least %s.\n"+
				"  Fix   free some space, or record elsewhere with --output <dir>\n"+
				"  Or    cap the recording with --max-size 200MB or --keep-last 5m",
			humanBytes(free), root, humanBytes(MinFreeBytes))
		return c
	}
	c.OK = true
	return c
}
