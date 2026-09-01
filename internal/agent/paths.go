package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type PathGuard struct {
	roots []string
}

func NewPathGuard(roots []string) (*PathGuard, error) {
	guard := &PathGuard{}
	for _, root := range roots {
		abs, err := canonicalPath(root)
		if err != nil {
			return nil, err
		}
		guard.roots = append(guard.roots, filepath.Clean(abs))
	}
	if len(guard.roots) == 0 {
		return nil, errors.New("no allowed filesystem roots configured")
	}
	return guard, nil
}

func (g *PathGuard) IsAllowed(path string) bool {
	abs, err := canonicalPath(path)
	if err != nil {
		return false
	}
	abs = filepath.Clean(abs)
	for _, root := range g.roots {
		if within(root, abs) {
			return true
		}
	}
	return false
}

func (g *PathGuard) allowedBase(path string) (string, string, error) {
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return "", "", errors.New("path must be absolute")
	}
	abs := filepath.Clean(path)
	base := ""
	for _, candidate := range g.roots {
		if within(candidate, abs) && len(candidate) > len(base) {
			base = candidate
		}
	}
	if base == "" {
		return "", "", errors.New("path is outside the configured filesystem boundary")
	}
	relative, err := filepath.Rel(base, abs)
	if err != nil {
		return "", "", err
	}
	return base, relative, nil
}

func (g *PathGuard) Resolve(root, relative string, mustExist bool) (string, error) {
	rootAbs, err := canonicalPath(root)
	if err != nil {
		return "", err
	}
	rootAbs = filepath.Clean(rootAbs)
	if !g.IsAllowed(rootAbs) {
		return "", errors.New("root is outside the configured filesystem boundary")
	}
	if filepath.IsAbs(relative) {
		return "", errors.New("path must be relative to its root")
	}
	target := filepath.Clean(filepath.Join(rootAbs, relative))
	if !within(rootAbs, target) {
		return "", errors.New("path escapes its project root")
	}
	check := target
	if !mustExist {
		check = filepath.Dir(target)
	}
	resolved, err := filepath.EvalSymlinks(check)
	if err != nil {
		if mustExist || !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		resolved = check
	}
	resolvedAbs, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	if !g.IsAllowed(resolvedAbs) || !within(rootAbs, resolvedAbs) {
		return "", errors.New("symlink resolves outside the configured filesystem boundary")
	}
	return target, nil
}

func (g *PathGuard) EnsureProjectRoot(path string) error {
	if !filepath.IsAbs(path) || !g.IsAllowed(path) {
		return errors.New("project root is outside the configured filesystem boundary")
	}
	parent := filepath.Dir(filepath.Clean(path))
	if !g.IsAllowed(parent) {
		return errors.New("project parent is outside the configured filesystem boundary")
	}
	if err := os.MkdirAll(path, 0o750); err != nil {
		return fmt.Errorf("create project root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if !g.IsAllowed(resolved) {
		return errors.New("project root resolves outside the configured filesystem boundary")
	}
	return nil
}

func within(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	current := abs
	var missing []string
	for {
		resolved, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(resolveErr, os.ErrNotExist) {
			return "", resolveErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return abs, nil
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}
