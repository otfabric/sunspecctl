// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Size limits for the files sunspecctl reads. They are far above what a
// real file needs and keep a wrong path (/dev/zero, a disk image) from
// exhausting memory.
const (
	// maxDeviceFileSize bounds a device file. A device with every register
	// of the address space given as a number takes about 1 MiB.
	maxDeviceFileSize = 16 << 20
	// maxPEMFileSize bounds a certificate, key or CA bundle. A system CA
	// bundle is a few hundred KiB.
	maxPEMFileSize = 4 << 20
)

// errTooLarge is returned by readFileLimited for a file over its limit.
var errTooLarge = errors.New("file too large")

// readFileLimited reads the file at path, failing with errTooLarge without
// reading further when it holds more than limit bytes. Errors name the path,
// as os.ReadFile's do.
func readFileLimited(path string, limit int64) ([]byte, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s: %w: more than %d MiB", path, errTooLarge, limit>>20)
	}
	return data, nil
}

// writeFileAtomic writes data to path as a new file with mode 0600: to a
// temporary file in the same directory first, which is then renamed over
// path, so that path holds either its old content or all of data. It refuses
// a path that is a symbolic link or not a regular file: the rename would
// replace the link or the device node, not write to what it names.
func writeFileAtomic(path string, data []byte) error {
	path = filepath.Clean(path)
	switch fi, err := os.Lstat(path); {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case fi.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("%s is a symbolic link: refusing to write through it", path)
	case !fi.Mode().IsRegular():
		return fmt.Errorf("%s is not a regular file", path)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

// rename is os.Rename; tests replace it to make the last step fail.
var rename = os.Rename
