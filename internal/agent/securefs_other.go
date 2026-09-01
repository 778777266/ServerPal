//go:build !linux

package agent

import (
	"errors"
	"os"
)

func secureOpen(guard *PathGuard, root, relative string) (*os.File, error) {
	path, err := guard.Resolve(root, relative, true)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func secureWrite(guard *PathGuard, root, relative string, data []byte, mode os.FileMode, uid, gid int, exclusive bool) error {
	path, err := guard.Resolve(root, relative, false)
	if err != nil {
		return err
	}
	if exclusive {
		if _, err := os.Lstat(path); err == nil {
			return errors.New("destination already exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := writeAtomic(path, data, mode); err != nil {
		return err
	}
	if uid >= 0 && gid >= 0 {
		return os.Chown(path, uid, gid)
	}
	return nil
}

func secureMkdir(guard *PathGuard, root, relative string, mode os.FileMode, uid, gid int) error {
	path, err := guard.Resolve(root, relative, false)
	if err != nil {
		return err
	}
	if err := os.Mkdir(path, mode); err != nil {
		return err
	}
	if uid >= 0 && gid >= 0 {
		return os.Chown(path, uid, gid)
	}
	return nil
}

func secureDirectoryNoSymlink(guard *PathGuard, root, relative string) error {
	path, err := guard.Resolve(root, relative, true)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("path is not a regular directory")
	}
	return nil
}

func secureRename(guard *PathGuard, root, from, to string) error {
	source, err := guard.Resolve(root, from, true)
	if err != nil {
		return err
	}
	destination, err := guard.Resolve(root, to, false)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, destination)
}

func secureEnsureProjectRoot(guard *PathGuard, path string, uid, gid int) error {
	if err := guard.EnsureProjectRoot(path); err != nil {
		return err
	}
	if uid >= 0 && gid >= 0 {
		return os.Chown(path, uid, gid)
	}
	return nil
}
