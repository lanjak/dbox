//go:build js || windows || plan9 || wasip1

package config

import "os"

func openConfigFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
}
