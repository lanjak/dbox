package cli

import (
	"errors"
	"io"
)

func Run(args []string, in io.Reader, out, errOut io.Writer) error {
	return errors.New("configuration is not set up")
}
