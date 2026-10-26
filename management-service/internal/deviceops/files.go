package deviceops

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func PrivatePath(root, key string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(key))
	if root == "" || filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" || strings.HasPrefix(strings.ReplaceAll(key, "\\", "/"), "/") || clean == "." || strings.HasPrefix(clean, "..") {
		return "", errors.New("设备文件路径无效")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, clean)
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("设备文件不能使用符号链接")
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", errors.New("设备文件路径无效")
	}
	for p := filepath.Dir(target); ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("设备文件目录不能使用符号链接")
		}
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if p == root {
			break
		}
		if filepath.Dir(p) == p {
			return "", errors.New("设备文件目录无效")
		}
	}
	return target, nil
}

// Private immutable, content-addressed batches. Sync before returning an ACK.
func SaveBatch(root, key string, raw []byte) error {
	target, err := PrivatePath(root, key)
	if err != nil {
		return err
	}
	if old, err := os.ReadFile(target); err == nil {
		if !bytes.Equal(old, raw) {
			return ErrConflict
		}
		return SyncPrivateDirectories(root, filepath.Dir(target))
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".batch-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := io.Copy(temp, bytes.NewReader(raw)); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, target); err != nil {
		if old, e := os.ReadFile(target); e == nil && bytes.Equal(old, raw) {
			return SyncPrivateDirectories(root, filepath.Dir(target))
		}
		return err
	}
	return SyncPrivateDirectories(root, filepath.Dir(target))
}

// Persist every newly created path component, including the private root's
// entry in its parent. Syncing just the leaf cannot make a new hierarchy durable.
func SyncPrivateDirectories(root, leaf string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	leaf, err = filepath.Abs(leaf)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, leaf)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("设备私有目录无效")
	}
	stop := filepath.Dir(root)
	for dir := leaf; ; dir = filepath.Dir(dir) {
		f, err := os.Open(dir)
		if err != nil {
			return err
		}
		err = f.Sync()
		closeErr := f.Close()
		if err != nil && runtime.GOOS != "windows" {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if dir == stop || filepath.Dir(dir) == dir {
			return nil
		}
	}
}
