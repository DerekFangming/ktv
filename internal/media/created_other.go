//go:build !darwin

package media

import (
	"os"
	"time"
)

func createdAt(info os.FileInfo) time.Time {
	return info.ModTime()
}
