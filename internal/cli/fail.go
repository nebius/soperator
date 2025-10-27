package cli

import (
	"os"

	"github.com/go-logr/logr"
)

func Fail(logger logr.Logger, err error, message string, keysAndValues ...any) {
	logger.Error(err, message, keysAndValues...)
	os.Exit(1)
}
