package proto

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	DefaultPort    = 7891
	ServiceName    = "_fcmd._tcp"
	ProtoVersion   = "1"
	MaxHeaderBytes = 1 << 20 // 1 MiB JSON header cap
	ChunkSize      = 256 * 1024
)

type Op string

const (
	OpList   Op = "list"
	OpStat   Op = "stat"
	OpMkdir  Op = "mkdir"
	OpRename Op = "rename"
	OpDelete Op = "delete"
	OpRead   Op = "read"
	OpWrite  Op = "write"
	OpRoots  Op = "roots"
)

type Request struct {
	Op        Op     `json:"op"`
	Path      string `json:"path,omitempty"`
	NewPath   string `json:"new_path,omitempty"`
	Recursive bool   `json:"recursive,omitempty"`
	Size      int64  `json:"size,omitempty"`
	Mode      uint32 `json:"mode,omitempty"`
}

type Entry struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"is_dir"`
	ModUnix int64  `json:"mod_unix"`
	Mode    uint32 `json:"mode,omitempty"`
}

type Response struct {
	OK      bool    `json:"ok"`
	Error   string  `json:"error,omitempty"`
	Entries []Entry `json:"entries,omitempty"`
	Entry   *Entry  `json:"entry,omitempty"`
	Size    int64   `json:"size,omitempty"`
	Roots   []Root  `json:"roots,omitempty"`
}

type Root struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func WriteJSON(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > MaxHeaderBytes {
		return errors.New("header too large")
	}
	var lb [4]byte
	binary.BigEndian.PutUint32(lb[:], uint32(len(b)))
	if _, err := w.Write(lb[:]); err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

func ReadJSON(r io.Reader, v any) error {
	var lb [4]byte
	if _, err := io.ReadFull(r, lb[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(lb[:])
	if n > MaxHeaderBytes {
		return fmt.Errorf("header too large: %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return err
	}
	return json.Unmarshal(buf, v)
}

// WriteChunk frames a binary chunk (length prefix). A zero-length chunk signals EOF.
func WriteChunk(w io.Writer, data []byte) error {
	var lb [4]byte
	binary.BigEndian.PutUint32(lb[:], uint32(len(data)))
	if _, err := w.Write(lb[:]); err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	_, err := w.Write(data)
	return err
}

// ReadChunk reads one chunk. Returns (nil, io.EOF) on a zero-length terminator.
func ReadChunk(r io.Reader, buf []byte) (int, error) {
	var lb [4]byte
	if _, err := io.ReadFull(r, lb[:]); err != nil {
		return 0, err
	}
	n := int(binary.BigEndian.Uint32(lb[:]))
	if n == 0 {
		return 0, io.EOF
	}
	if n > cap(buf) {
		return 0, fmt.Errorf("chunk too large: %d > %d", n, cap(buf))
	}
	return io.ReadFull(r, buf[:n])
}
