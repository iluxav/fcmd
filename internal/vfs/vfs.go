package vfs

import (
	"io"
	"path"
	"strings"

	"fcmd/internal/proto"
)

type Entry = proto.Entry

// FS is a minimal file system abstraction backed by either the local disk
// or a remote fcmd daemon over the wire.
type FS interface {
	Label() string
	IsRemote() bool
	Roots() ([]proto.Root, error)
	List(p string) ([]Entry, error)
	Stat(p string) (*Entry, error)
	Mkdir(p string) error
	Rename(oldPath, newPath string) error
	Delete(p string, recursive bool) error
	OpenRead(p string) (io.ReadCloser, int64, error)
	OpenWrite(p string, size int64) (io.WriteCloser, error)
	Join(a, b string) string
	Parent(p string) string
	Close() error
}

// PosixJoin performs path-style join that stays canonical.
func PosixJoin(a, b string) string {
	if a == "" {
		return b
	}
	return path.Join(a, b)
}

// PosixParent returns the parent directory of a posix-style path.
func PosixParent(p string) string {
	p = strings.TrimRight(p, "/")
	if p == "" || p == "/" {
		return "/"
	}
	parent := path.Dir(p)
	return parent
}
