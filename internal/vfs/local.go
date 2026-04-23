package vfs

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"fcmd/internal/proto"
)

type Local struct{}

func NewLocal() *Local { return &Local{} }

func (l *Local) Label() string   { return "Local" }
func (l *Local) IsRemote() bool  { return false }
func (l *Local) Close() error    { return nil }
func (l *Local) Join(a, b string) string {
	return filepath.Join(a, b)
}
func (l *Local) Parent(p string) string {
	parent := filepath.Dir(p)
	if parent == p {
		return p
	}
	return parent
}

func (l *Local) Roots() ([]proto.Root, error) {
	home, _ := os.UserHomeDir()
	out := []proto.Root{}
	if home != "" {
		out = append(out, proto.Root{Name: "Home", Path: home})
	}
	if runtime.GOOS == "windows" {
		if sd := os.Getenv("SystemDrive"); sd != "" {
			out = append(out, proto.Root{Name: sd + `\`, Path: sd + `\`})
		}
	} else {
		out = append(out, proto.Root{Name: "/", Path: "/"})
	}
	return out, nil
}

func (l *Local) List(p string) ([]Entry, error) {
	ents, err := os.ReadDir(p)
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

func (l *Local) Stat(p string) (*Entry, error) {
	info, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	return &Entry{
		Name:    filepath.Base(p),
		Size:    info.Size(),
		IsDir:   info.IsDir(),
		ModUnix: info.ModTime().Unix(),
		Mode:    uint32(info.Mode()),
	}, nil
}

func (l *Local) Mkdir(p string) error {
	return os.MkdirAll(p, 0o755)
}

func (l *Local) Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func (l *Local) Delete(p string, recursive bool) error {
	if recursive {
		return os.RemoveAll(p)
	}
	return os.Remove(p)
}

func (l *Local) OpenRead(p string) (io.ReadCloser, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

func (l *Local) OpenWrite(p string, size int64) (io.WriteCloser, error) {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	tmp := p + ".fcmd.part"
	f, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}
	return &atomicFile{f: f, tmp: tmp, final: p}, nil
}

type atomicFile struct {
	f     *os.File
	tmp   string
	final string
}

func (a *atomicFile) Write(p []byte) (int, error) { return a.f.Write(p) }
func (a *atomicFile) Close() error {
	if err := a.f.Close(); err != nil {
		os.Remove(a.tmp)
		return err
	}
	return os.Rename(a.tmp, a.final)
}
