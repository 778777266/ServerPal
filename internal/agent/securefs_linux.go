//go:build linux

package agent

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const secureResolve uint64 = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_SYMLINKS

func secureOpen(guard *PathGuard, root, relative string) (*os.File, error) {
	rootFD, err := secureRootFD(guard, root)
	if err != nil {
		return nil, err
	}
	defer unix.Close(rootFD)
	clean, err := cleanSecureRelative(relative, true)
	if err != nil {
		return nil, err
	}
	fd, err := openAtSecure(rootFD, clean, unix.O_RDONLY|unix.O_CLOEXEC)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), filepath.Base(clean)), nil
}

func secureWrite(guard *PathGuard, root, relative string, data []byte, mode os.FileMode, uid, gid int, exclusive bool) error {
	rootFD, err := secureRootFD(guard, root)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	parentFD, name, err := secureParentFD(rootFD, relative)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	temporary := fmt.Sprintf(".serverpal-%x", random)
	fd, err := unix.Openat(parentFD, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode.Perm()))
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), temporary)
	cleanup := true
	defer func() {
		file.Close()
		if cleanup {
			_ = unix.Unlinkat(parentFD, temporary, 0)
		}
	}()
	if uid >= 0 && gid >= 0 {
		if err := unix.Fchown(fd, uid, gid); err != nil {
			return err
		}
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if exclusive {
		err = renameAtNoReplace(parentFD, temporary, parentFD, name)
	} else {
		err = unix.Renameat(parentFD, temporary, parentFD, name)
	}
	if err != nil {
		return err
	}
	cleanup = false
	return unix.Fsync(parentFD)
}

func secureMkdir(guard *PathGuard, root, relative string, mode os.FileMode, uid, gid int) error {
	rootFD, err := secureRootFD(guard, root)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	parentFD, name, err := secureParentFD(rootFD, relative)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	if err := unix.Mkdirat(parentFD, name, uint32(mode.Perm())); err != nil {
		return err
	}
	if uid >= 0 && gid >= 0 {
		if err := unix.Fchownat(parentFD, name, uid, gid, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
	}
	return unix.Fsync(parentFD)
}

func secureDirectoryNoSymlink(guard *PathGuard, root, relative string) error {
	rootFD, err := secureRootFD(guard, root)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	clean, err := cleanSecureRelative(relative, false)
	if err != nil {
		return err
	}
	fd, err := openAtSecure(rootFD, clean, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC)
	if err != nil {
		return err
	}
	return unix.Close(fd)
}

func secureRename(guard *PathGuard, root, from, to string) error {
	rootFD, err := secureRootFD(guard, root)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	sourceParent, sourceName, err := secureParentFD(rootFD, from)
	if err != nil {
		return err
	}
	defer unix.Close(sourceParent)
	destinationParent, destinationName, err := secureParentFD(rootFD, to)
	if err != nil {
		return err
	}
	defer unix.Close(destinationParent)
	if err := renameAtNoReplace(sourceParent, sourceName, destinationParent, destinationName); err != nil {
		return err
	}
	if err := unix.Fsync(sourceParent); err != nil {
		return err
	}
	if sourceParent != destinationParent {
		return unix.Fsync(destinationParent)
	}
	return nil
}

func secureEnsureProjectRoot(guard *PathGuard, path string, uid, gid int) error {
	base, relative, err := guard.allowedBase(path)
	if err != nil {
		return err
	}
	clean, err := cleanSecureRelative(relative, false)
	if err != nil {
		return err
	}
	baseFD, err := unix.Open(base, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(baseFD)
	currentFD := baseFD
	for _, component := range strings.Split(clean, "/") {
		fd, openErr := openAtSecure(currentFD, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC)
		if errors.Is(openErr, unix.ENOENT) {
			if err := unix.Mkdirat(currentFD, component, 0o750); err != nil && !errors.Is(err, unix.EEXIST) {
				if currentFD != baseFD {
					unix.Close(currentFD)
				}
				return err
			}
			fd, openErr = openAtSecure(currentFD, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC)
		}
		if openErr != nil {
			if currentFD != baseFD {
				unix.Close(currentFD)
			}
			return openErr
		}
		if err := unix.Fchmod(fd, 0o750); err != nil {
			unix.Close(fd)
			if currentFD != baseFD {
				unix.Close(currentFD)
			}
			return err
		}
		if uid >= 0 && gid >= 0 {
			if err := unix.Fchown(fd, uid, gid); err != nil {
				unix.Close(fd)
				if currentFD != baseFD {
					unix.Close(currentFD)
				}
				return err
			}
		}
		if currentFD != baseFD {
			unix.Close(currentFD)
		}
		currentFD = fd
	}
	defer unix.Close(currentFD)
	return nil
}

func secureRootFD(guard *PathGuard, root string) (int, error) {
	base, relative, err := guard.allowedBase(root)
	if err != nil {
		return -1, err
	}
	baseFD, err := unix.Open(base, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	if relative == "." {
		return baseFD, nil
	}
	clean, err := cleanSecureRelative(relative, false)
	if err != nil {
		unix.Close(baseFD)
		return -1, err
	}
	fd, err := openAtSecure(baseFD, clean, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC)
	unix.Close(baseFD)
	return fd, err
}

func secureParentFD(rootFD int, relative string) (int, string, error) {
	clean, err := cleanSecureRelative(relative, false)
	if err != nil {
		return -1, "", err
	}
	directory, name := filepath.Split(clean)
	directory = strings.TrimSuffix(filepath.ToSlash(directory), "/")
	if directory == "" {
		directory = "."
	}
	fd, err := openAtSecure(rootFD, directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC)
	return fd, name, err
}

func openAtSecure(directoryFD int, path string, flags int) (int, error) {
	fd, err := unix.Openat2(directoryFD, path, &unix.OpenHow{Flags: uint64(flags), Resolve: secureResolve})
	if err == nil || (!errors.Is(err, unix.ENOSYS) && !errors.Is(err, unix.EPERM) && !errors.Is(err, unix.EINVAL)) {
		return fd, err
	}
	currentFD, err := unix.Openat(directoryFD, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	components := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	for index, component := range components {
		componentFlags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		if index == len(components)-1 {
			componentFlags = flags | unix.O_NOFOLLOW
		}
		nextFD, openErr := unix.Openat(currentFD, component, componentFlags, 0)
		unix.Close(currentFD)
		if openErr != nil {
			return -1, openErr
		}
		currentFD = nextFD
	}
	return currentFD, nil
}

func renameAtNoReplace(oldDirectory int, oldName string, newDirectory int, newName string) error {
	err := unix.Renameat2(oldDirectory, oldName, newDirectory, newName, unix.RENAME_NOREPLACE)
	if err == nil || (!errors.Is(err, unix.ENOSYS) && !errors.Is(err, unix.EPERM) && !errors.Is(err, unix.EINVAL)) {
		return err
	}
	var destination unix.Stat_t
	if statErr := unix.Fstatat(newDirectory, newName, &destination, unix.AT_SYMLINK_NOFOLLOW); statErr == nil {
		return unix.EEXIST
	} else if !errors.Is(statErr, unix.ENOENT) {
		return statErr
	}
	return unix.Renameat(oldDirectory, oldName, newDirectory, newName)
}

func cleanSecureRelative(value string, allowRoot bool) (string, error) {
	if filepath.IsAbs(value) || strings.ContainsRune(value, 0) {
		return "", errors.New("path must be relative")
	}
	clean := filepath.ToSlash(filepath.Clean(value))
	if clean == "." {
		if allowRoot {
			return clean, nil
		}
		return "", errors.New("operation cannot target the project root")
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("path escapes its project root")
	}
	return clean, nil
}
