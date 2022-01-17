package util

import "os"

// isFileExists returns true if a file exists with the given path
func IsFileExists(path string) bool {
	f, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return !f.IsDir()
}
