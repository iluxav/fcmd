package proto

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type Client struct {
	addr string
	mu   sync.Mutex
	conn net.Conn
	br   *bufio.Reader
	bw   *bufio.Writer
}

func Dial(addr string) (*Client, error) {
	c := &Client{addr: addr}
	if err := c.connect(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Client) connect() error {
	conn, err := net.DialTimeout("tcp", c.addr, 5*time.Second)
	if err != nil {
		return err
	}
	c.conn = conn
	c.br = bufio.NewReaderSize(conn, 64*1024)
	c.bw = bufio.NewWriterSize(conn, 64*1024)
	return nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) call(req Request) (*Response, error) {
	if err := WriteJSON(c.bw, req); err != nil {
		return nil, err
	}
	if err := c.bw.Flush(); err != nil {
		return nil, err
	}
	var resp Response
	if err := ReadJSON(c.br, &resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return &resp, errors.New(resp.Error)
	}
	return &resp, nil
}

func (c *Client) List(path string) ([]Entry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	resp, err := c.call(Request{Op: OpList, Path: path})
	if err != nil {
		return nil, err
	}
	return resp.Entries, nil
}

func (c *Client) Stat(path string) (*Entry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	resp, err := c.call(Request{Op: OpStat, Path: path})
	if err != nil {
		return nil, err
	}
	return resp.Entry, nil
}

func (c *Client) Mkdir(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.call(Request{Op: OpMkdir, Path: path})
	return err
}

func (c *Client) Rename(oldPath, newPath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.call(Request{Op: OpRename, Path: oldPath, NewPath: newPath})
	return err
}

func (c *Client) Delete(path string, recursive bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.call(Request{Op: OpDelete, Path: path, Recursive: recursive})
	return err
}

func (c *Client) Roots() ([]Root, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	resp, err := c.call(Request{Op: OpRoots})
	if err != nil {
		return nil, err
	}
	return resp.Roots, nil
}

// ReadFile streams the file to w, invoking onProgress with bytes delivered so far.
func (c *Client) ReadFile(path string, w io.Writer, onProgress func(n int64)) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := WriteJSON(c.bw, Request{Op: OpRead, Path: path}); err != nil {
		return 0, err
	}
	if err := c.bw.Flush(); err != nil {
		return 0, err
	}
	var resp Response
	if err := ReadJSON(c.br, &resp); err != nil {
		return 0, err
	}
	if !resp.OK {
		return 0, errors.New(resp.Error)
	}
	buf := make([]byte, ChunkSize)
	var total int64
	for {
		n, err := ReadChunk(c.br, buf)
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
		if _, werr := w.Write(buf[:n]); werr != nil {
			return total, werr
		}
		total += int64(n)
		if onProgress != nil {
			onProgress(total)
		}
	}
}

// WriteFile streams r to the remote path with the given size.
func (c *Client) WriteFile(path string, size int64, r io.Reader, onProgress func(n int64)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := WriteJSON(c.bw, Request{Op: OpWrite, Path: path, Size: size}); err != nil {
		return err
	}
	if err := c.bw.Flush(); err != nil {
		return err
	}
	buf := make([]byte, ChunkSize)
	var total int64
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if werr := WriteChunk(c.bw, buf[:n]); werr != nil {
				return werr
			}
			total += int64(n)
			if onProgress != nil {
				onProgress(total)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if err := WriteChunk(c.bw, nil); err != nil {
		return err
	}
	if err := c.bw.Flush(); err != nil {
		return err
	}
	var resp Response
	if err := ReadJSON(c.br, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("write failed: %s", resp.Error)
	}
	return nil
}
