package build

import "os"

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
