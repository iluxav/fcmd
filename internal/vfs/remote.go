package vfs

import (
	"io"
	"path"
	"strings"

	"fcmd/internal/proto"
)

type Remote struct {
	addr   string
	client *proto.Client // shared connection for short (non-streaming) RPCs
	label  string
}

func NewRemote(addr, label string) (*Remote, error) {
	c, err := proto.Dial(addr)
	if err != nil {
		return nil, err
	}
	return &Remote{addr: addr, client: c, label: label}, nil
}

func (r *Remote) Label() string   { return r.label }
func (r *Remote) IsRemote() bool  { return true }
func (r *Remote) Close() error    { return r.client.Close() }

// Remote paths are normalized to posix style. Daemons on Windows still receive
// forward-slash paths and accept them via the os package.
func (r *Remote) Join(a, b string) string {
	if a == "" {
		return b
	}
	return path.Join(a, b)
}

func (r *Remote) Parent(p string) string {
	p = strings.TrimRight(p, "/")
	if p == "" {
		return "/"
	}
	parent := path.Dir(p)
	return parent
}

func (r *Remote) Roots() ([]proto.Root, error) { return r.client.Roots() }
func (r *Remote) List(p string) ([]Entry, error) { return r.client.List(p) }
func (r *Remote) Stat(p string) (*Entry, error)  { return r.client.Stat(p) }
func (r *Remote) Mkdir(p string) error           { return r.client.Mkdir(p) }
func (r *Remote) Rename(oldPath, newPath string) error {
	return r.client.Rename(oldPath, newPath)
}
func (r *Remote) Delete(p string, recursive bool) error {
	return r.client.Delete(p, recursive)
}

// OpenRead dials a dedicated connection for the stream. Using a fresh client
// avoids contention with the shared client's mutex and lets reads/writes
// against the same daemon proceed concurrently (needed for remote->remote copy).
func (r *Remote) OpenRead(p string) (io.ReadCloser, int64, error) {
	st, err := r.client.Stat(p)
	if err != nil {
		return nil, 0, err
	}
	stream, err := proto.Dial(r.addr)
	if err != nil {
		return nil, 0, err
	}
	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		_, err := stream.ReadFile(p, pw, nil)
		pw.CloseWithError(err)
		stream.Close()
		errCh <- err
	}()
	return &remoteReader{pr: pr, errCh: errCh}, st.Size, nil
}

type remoteReader struct {
	pr    *io.PipeReader
	errCh chan error
}

func (r *remoteReader) Read(p []byte) (int, error) { return r.pr.Read(p) }
func (r *remoteReader) Close() error {
	err := r.pr.Close()
	<-r.errCh
	return err
}

func (r *Remote) OpenWrite(p string, size int64) (io.WriteCloser, error) {
	stream, err := proto.Dial(r.addr)
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		err := stream.WriteFile(p, size, pr, nil)
		pr.CloseWithError(err)
		stream.Close()
		errCh <- err
	}()
	return &remoteWriter{pw: pw, errCh: errCh}, nil
}

type remoteWriter struct {
	pw    *io.PipeWriter
	errCh chan error
}

func (w *remoteWriter) Write(p []byte) (int, error) { return w.pw.Write(p) }
func (w *remoteWriter) Close() error {
	if err := w.pw.Close(); err != nil {
		return err
	}
	return <-w.errCh
}
