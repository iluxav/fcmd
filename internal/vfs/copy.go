package vfs

import (
	"fmt"
	"io"
	"path/filepath"
)

// Progress is a callback used to report copy progress.
// filesDone/totalFiles count whole items; bytesDone/totalBytes count bytes.
type Progress func(bytesDone, totalBytes, filesDone, totalFiles int64, currentName string, currentBytes, currentTotal int64)

// Plan walks a source path and returns total file count + total bytes.
// Directories are counted as zero-size files.
func Plan(src FS, path string) (files, bytes int64, err error) {
	st, err := src.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	if !st.IsDir {
		return 1, st.Size, nil
	}
	var f, b int64
	f++ // the dir itself
	entries, err := src.List(path)
	if err != nil {
		return 0, 0, err
	}
	for _, e := range entries {
		sub := src.Join(path, e.Name)
		cf, cb, err := Plan(src, sub)
		if err != nil {
			return 0, 0, err
		}
		f += cf
		b += cb
	}
	return f, b, nil
}

type progressWriter struct {
	w        io.Writer
	progress Progress
	counter  *counters
	total    int64
	fileSize int64
	thisDone int64
	thisName string
}

type counters struct {
	bytes int64
	files int64
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	if n > 0 && p.progress != nil {
		p.counter.bytes += int64(n)
		p.thisDone += int64(n)
		p.progress(p.counter.bytes, p.total, p.counter.files, -1, p.thisName, p.thisDone, p.fileSize)
	}
	return n, err
}

// Copy copies src.path to dst at dstPath (which should be the destination item path).
// When copying a directory, children are placed under dstPath/<child-name>.
func Copy(src, dst FS, srcPath, dstPath string, totalFiles, totalBytes int64, progress Progress) error {
	st, err := src.Stat(srcPath)
	if err != nil {
		return err
	}
	c := &counters{}
	return copyTree(src, dst, srcPath, dstPath, st, c, totalFiles, totalBytes, progress)
}

func copyTree(src, dst FS, srcPath, dstPath string, st *Entry, c *counters, totalFiles, totalBytes int64, progress Progress) error {
	if st.IsDir {
		if err := dst.Mkdir(dstPath); err != nil {
			return fmt.Errorf("mkdir %s: %w", dstPath, err)
		}
		c.files++
		if progress != nil {
			progress(c.bytes, totalBytes, c.files, totalFiles, filepath.Base(dstPath), 0, 0)
		}
		entries, err := src.List(srcPath)
		if err != nil {
			return err
		}
		for _, e := range entries {
			ss := src.Join(srcPath, e.Name)
			ds := dst.Join(dstPath, e.Name)
			sst, err := src.Stat(ss)
			if err != nil {
				return err
			}
			if err := copyTree(src, dst, ss, ds, sst, c, totalFiles, totalBytes, progress); err != nil {
				return err
			}
		}
		return nil
	}
	rc, size, err := src.OpenRead(srcPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", srcPath, err)
	}
	defer rc.Close()
	wc, err := dst.OpenWrite(dstPath, size)
	if err != nil {
		return fmt.Errorf("create %s: %w", dstPath, err)
	}
	pw := &progressWriter{
		w:        wc,
		progress: progress,
		counter:  c,
		total:    totalBytes,
		fileSize: size,
		thisName: filepath.Base(srcPath),
	}
	if _, err := io.Copy(pw, rc); err != nil {
		wc.Close()
		return fmt.Errorf("copy %s: %w", srcPath, err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dstPath, err)
	}
	c.files++
	if progress != nil {
		progress(c.bytes, totalBytes, c.files, totalFiles, filepath.Base(dstPath), size, size)
	}
	return nil
}
