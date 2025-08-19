package sconfigcontroller

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func isRenameNotSupported(err error) bool {
	return errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.ENOSYS)
}

func renameExchange(oldPath, newPath string) error {
	if err := unix.Renameat2(unix.AT_FDCWD, oldPath, unix.AT_FDCWD, newPath, unix.RENAME_EXCHANGE); err != nil {
		if isRenameNotSupported(err) {
			log.Log.V(1).Info("renameat2 exchange not supported; falling back to os.Rename", "oldPath", oldPath, "newPath", newPath, "err", err)
			return os.Rename(oldPath, newPath)
		}
		return err
	}
	return nil
}

func renameNoReplace(oldPath, newPath string) error {
	if err := unix.Renameat2(unix.AT_FDCWD, oldPath, unix.AT_FDCWD, newPath, unix.RENAME_NOREPLACE); err != nil {
		if isRenameNotSupported(err) {
			log.Log.V(1).Info("renameat2 noreplace not supported; falling back to os.Rename", "oldPath", oldPath, "newPath", newPath, "err", err)
			return os.Rename(oldPath, newPath)
		}
		return err
	}
	return nil
}
