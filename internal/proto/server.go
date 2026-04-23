package proto

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
)

// RootsProvider returns the advertised top-level roots of the daemon host.
type RootsProvider func() []Root

type Server struct {
	Listener net.Listener
	Roots    RootsProvider
}

func (s *Server) Serve() error {
	for {
		conn, err := s.Listener.Accept()
		if err != nil {
			return err
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReaderSize(conn, 64*1024)
	bw := bufio.NewWriterSize(conn, 64*1024)
	for {
		var req Request
		if err := ReadJSON(br, &req); err != nil {
			if err != io.EOF {
				log.Printf("read request: %v", err)
			}
			return
		}
		if err := s.dispatch(req, br, bw); err != nil {
			log.Printf("dispatch %s %q: %v", req.Op, req.Path, err)
			writeErr(bw, err)
		}
		if err := bw.Flush(); err != nil {
			log.Printf("flush: %v", err)
			return
		}
	}
}

func writeErr(w io.Writer, err error) {
	_ = WriteJSON(w, Response{OK: false, Error: err.Error()})
}

func (s *Server) dispatch(req Request, r *bufio.Reader, w *bufio.Writer) error {
	switch req.Op {
	case OpRoots:
		var roots []Root
		if s.Roots != nil {
			roots = s.Roots()
		}
		return WriteJSON(w, Response{OK: true, Roots: roots})
	case OpList:
		entries, err := listDir(req.Path)
		if err != nil {
			return err
		}
		return WriteJSON(w, Response{OK: true, Entries: entries})
	case OpStat:
		e, err := statPath(req.Path)
		if err != nil {
			return err
		}
		return WriteJSON(w, Response{OK: true, Entry: e})
	case OpMkdir:
		if err := os.MkdirAll(req.Path, 0o755); err != nil {
			return err
		}
		return WriteJSON(w, Response{OK: true})
	case OpRename:
		if req.NewPath == "" {
			return errors.New("new_path required")
		}
		if err := os.Rename(req.Path, req.NewPath); err != nil {
			return err
		}
		return WriteJSON(w, Response{OK: true})
	case OpDelete:
		var err error
		if req.Recursive {
			err = os.RemoveAll(req.Path)
		} else {
			err = os.Remove(req.Path)
		}
		if err != nil {
			return err
		}
		return WriteJSON(w, Response{OK: true})
	case OpRead:
		return serveRead(req.Path, w)
	case OpWrite:
		return serveWrite(req.Path, req.Size, r, w)
	default:
		return fmt.Errorf("unknown op: %s", req.Op)
	}
}

func listDir(path string) ([]Entry, error) {
	ents, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(ents))
	for _, de := range ents {
		info, err := de.Info()
		if err != nil {
			continue
		}
		out = append(out, Entry{
			Name:    de.Name(),
			Size:    info.Size(),
			IsDir:   de.IsDir(),
			ModUnix: info.ModTime().Unix(),
			Mode:    uint32(info.Mode()),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func statPath(path string) (*Entry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &Entry{
		Name:    filepath.Base(path),
		Size:    info.Size(),
		IsDir:   info.IsDir(),
		ModUnix: info.ModTime().Unix(),
		Mode:    uint32(info.Mode()),
	}, nil
}

func serveRead(path string, w *bufio.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err := WriteJSON(w, Response{OK: true, Size: info.Size()}); err != nil {
		return err
	}
	buf := make([]byte, ChunkSize)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if werr := WriteChunk(w, buf[:n]); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return WriteChunk(w, nil)
}

func serveWrite(path string, size int64, r *bufio.Reader, w *bufio.Writer) error {
	var f *os.File
	var tmp string
	var writeErr error

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		writeErr = err
	} else {
		tmp = path + ".fcmd.part"
		file, err := os.Create(tmp)
		if err != nil {
			writeErr = err
		} else {
			f = file
		}
	}

	// Always fully drain the incoming chunks so the connection stays in sync
	// even if the underlying write failed.
	buf := make([]byte, ChunkSize)
	var total int64
	for {
		n, err := ReadChunk(r, buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			if f != nil {
				f.Close()
				os.Remove(tmp)
			}
			return err
		}
		if f != nil && writeErr == nil {
			if _, werr := f.Write(buf[:n]); werr != nil {
				writeErr = werr
			}
		}
		total += int64(n)
	}
	if f != nil {
		if cerr := f.Close(); cerr != nil && writeErr == nil {
			writeErr = cerr
		}
	}
	if writeErr != nil {
		if tmp != "" {
			os.Remove(tmp)
		}
		return writeErr
	}
	if size > 0 && total != size {
		os.Remove(tmp)
		return fmt.Errorf("size mismatch: got %d want %d", total, size)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return WriteJSON(w, Response{OK: true})
}
