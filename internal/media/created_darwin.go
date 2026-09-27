//go:build darwin

package media

import (
	"os"
	"syscall"
	"time"
)

func createdAt(info os.FileInfo) time.Time {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info.ModTime()
	}
	sec, nsec := stat.Birthtimespec.Unix()
	if sec == 0 && nsec == 0 {
		return info.ModTime()
	}
	return time.Unix(sec, nsec)
}
