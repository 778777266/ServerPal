package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	maxEditableFile = 2 << 20
	maxUploadFile   = 64 << 20
)

type FileService struct {
	guard *PathGuard
	uid   int
	gid   int
}

func NewFileService(guard *PathGuard, projectUser string) (*FileService, error) {
	service := &FileService{guard: guard, uid: -1, gid: -1}
	if projectUser == "" {
		return service, nil
	}
	u, err := user.Lookup(projectUser)
	if err != nil {
		return nil, fmt.Errorf("lookup project user: %w", err)
	}
	service.uid, err = strconv.Atoi(u.Uid)
	if err != nil {
		return nil, fmt.Errorf("parse project user uid: %w", err)
	}
	service.gid, err = strconv.Atoi(u.Gid)
	if err != nil {
		return nil, fmt.Errorf("parse project user gid: %w", err)
	}
	return service, nil
}

func (s *FileService) List(root, relative string) (FileList, error) {
	directory, err := secureOpen(s.guard, root, relative)
	if err != nil {
		return FileList{}, err
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil {
		return FileList{}, err
	}
	if !info.IsDir() {
		return FileList{}, errors.New("path is not a directory")
	}
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return FileList{}, err
	}
	result := FileList{Root: filepath.Clean(root), Path: cleanRelative(relative), Entries: make([]FileEntry, 0, len(entries))}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		kind := "file"
		if entry.IsDir() {
			kind = "directory"
		} else if info.Mode()&os.ModeSymlink != 0 {
			kind = "symlink"
		}
		result.Entries = append(result.Entries, FileEntry{
			Name:       entry.Name(),
			Path:       cleanRelative(filepath.Join(relative, entry.Name())),
			Type:       kind,
			Size:       info.Size(),
			Mode:       info.Mode().String(),
			Owner:      fileOwner(info),
			ModifiedAt: info.ModTime(),
		})
	}
	sort.Slice(result.Entries, func(i, j int) bool {
		if result.Entries[i].Type != result.Entries[j].Type {
			return result.Entries[i].Type == "directory"
		}
		return strings.ToLower(result.Entries[i].Name) < strings.ToLower(result.Entries[j].Name)
	})
	return result, nil
}

func (s *FileService) ReadContent(root, relative string) ([]byte, os.FileMode, error) {
	file, err := secureOpen(s.guard, root, relative)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, errors.New("path is not a regular file")
	}
	if info.Size() > maxEditableFile {
		return nil, 0, errors.New("file is too large for the web editor")
	}
	b, err := io.ReadAll(io.LimitReader(file, maxEditableFile+1))
	if err != nil {
		return nil, 0, err
	}
	if !utf8.Valid(b) || bytesContainZero(b) {
		return nil, 0, errors.New("binary files cannot be edited in the browser")
	}
	return b, info.Mode().Perm(), nil
}

func (s *FileService) WriteContent(root, relative string, content []byte) error {
	if len(content) > maxEditableFile {
		return errors.New("file is too large for the web editor")
	}
	if !utf8.Valid(content) || bytesContainZero(content) {
		return errors.New("content must be UTF-8 text")
	}
	mode := os.FileMode(0o640)
	if file, openErr := secureOpen(s.guard, root, relative); openErr == nil {
		info, statErr := file.Stat()
		file.Close()
		if statErr != nil {
			return statErr
		}
		if !info.Mode().IsRegular() {
			return errors.New("path is not a regular file")
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(openErr, os.ErrNotExist) {
		return openErr
	}
	return secureWrite(s.guard, root, relative, content, mode, s.uid, s.gid, false)
}

func (s *FileService) Upload(root, relative string, reader io.Reader) error {
	limited := io.LimitReader(reader, maxUploadFile+1)
	b, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if len(b) > maxUploadFile {
		return errors.New("upload exceeds the 64 MiB limit")
	}
	if err := secureWrite(s.guard, root, relative, b, 0o640, s.uid, s.gid, true); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("destination already exists")
		}
		return err
	}
	return nil
}

func (s *FileService) Mkdir(root, relative string) error {
	return secureMkdir(s.guard, root, relative, 0o750, s.uid, s.gid)
}

func (s *FileService) Rename(root, from, to string) error {
	if cleanRelative(from) == "" {
		return errors.New("project root cannot be renamed")
	}
	return secureRename(s.guard, root, from, to)
}

func (s *FileService) Trash(root, relative string) (string, error) {
	if relative == "" || relative == "." || strings.HasPrefix(cleanRelative(relative), ".serverpal-trash") {
		return "", errors.New("this path cannot be moved to trash")
	}
	if err := secureMkdir(s.guard, root, ".serverpal-trash", 0o700, s.uid, s.gid); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	if err := secureDirectoryNoSymlink(s.guard, root, ".serverpal-trash"); err != nil {
		return "", errors.New("project trash path is not a directory")
	}
	name := fmt.Sprintf("%s-%d-%s", time.Now().Format("20060102-150405"), time.Now().UnixNano()%1000000, filepath.Base(relative))
	destination := cleanRelative(filepath.Join(".serverpal-trash", name))
	if err := secureRename(s.guard, root, relative, destination); err != nil {
		return "", err
	}
	return destination, nil
}

func (s *FileService) OpenDownload(root, relative string) (*os.File, os.FileInfo, error) {
	file, err := secureOpen(s.guard, root, relative)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, nil, errors.New("only regular files can be downloaded")
	}
	return file, info, nil
}

func (s *FileService) EnsureProjectRoot(path string) error {
	return secureEnsureProjectRoot(s.guard, path, s.uid, s.gid)
}

func cleanRelative(value string) string {
	clean := filepath.Clean(value)
	if clean == "." {
		return ""
	}
	return filepath.ToSlash(clean)
}

func fileOwner(info os.FileInfo) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	uid := strconv.FormatUint(uint64(stat.Uid), 10)
	gid := strconv.FormatUint(uint64(stat.Gid), 10)
	owner, err := user.LookupId(uid)
	if err == nil {
		uid = owner.Username
	}
	group, err := user.LookupGroupId(gid)
	if err == nil {
		gid = group.Name
	}
	return uid + ":" + gid
}

func bytesContainZero(value []byte) bool {
	for _, b := range value {
		if b == 0 {
			return true
		}
	}
	return false
}
