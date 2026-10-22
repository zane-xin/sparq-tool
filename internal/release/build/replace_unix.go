//go:build !windows

package build

import "os"

func replaceFile(source string, target string) error {
	return os.Rename(source, target)
}
