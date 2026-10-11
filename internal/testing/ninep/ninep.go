// Package ninep provides a synchronous 9P2000 client for testing.
// Unlike go9p's client, this keeps one request in flight, so every method
// returns after the server has answered.
package ninep

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/knusbaum/go9p/proto"
)

// Fixed overheads from intro(5): size[4] type[1] tag[2], plus count[4] for
// Rread, and fid[4] offset[8] count[4] for Twrite.
const (
	rreadOverhead  = 11
	twriteOverhead = 23
)

// Conn is a 9P2000 client with one request in flight, so every method
// returns after the server has answered.
type Conn struct {
	rw    io.ReadWriter
	msize uint32
	mu    sync.Mutex
	fids  atomic.Uint32
}

// Dial connects to a 9P server, negotiates version, and attaches as the given user.
func Dial(rw io.ReadWriter, user string) (*Conn, error) {
	c := &Conn{rw: rw}

	rver, err := rpc[*proto.TRVersion](c, &proto.TRVersion{
		Header:  proto.Header{Type: proto.Tversion, Tag: 0},
		Msize:   65536,
		Version: "9P2000",
	})
	if err != nil {
		return nil, err
	}
	if rver.Version != "9P2000" {
		return nil, fmt.Errorf("server speaks %q, want 9P2000", rver.Version)
	}
	if rver.Msize <= twriteOverhead {
		return nil, fmt.Errorf("server msize %d leaves no room for data", rver.Msize)
	}
	c.msize = rver.Msize

	if _, err := rpc[*proto.RAttach](c, &proto.TAttach{
		Header: proto.Header{Type: proto.Tattach, Tag: 1},
		Fid:    0,
		Afid:   ^uint32(0),
		Uname:  user,
		Aname:  "",
	}); err != nil {
		return nil, err
	}

	c.fids.Store(1)
	return c, nil
}

// rpc sends req and waits for a reply of type R. An Rerror comes back as an
// error carrying its ename.
func rpc[R proto.FCall](c *Conn, req proto.FCall) (R, error) {
	var zero R
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := c.rw.Write(req.Compose()); err != nil {
		return zero, fmt.Errorf("write %T: %w", req, err)
	}
	call, err := proto.ParseCall(c.rw)
	if err != nil {
		return zero, fmt.Errorf("read reply to %T: %w", req, err)
	}
	if rerr, ok := call.(*proto.RError); ok {
		return zero, errors.New(rerr.Ename)
	}
	r, ok := call.(R)
	if !ok {
		return zero, fmt.Errorf("expected %T, got %T", zero, call)
	}
	return r, nil
}

// Fid represents an open file in the 9P server.
type Fid struct {
	conn *Conn
	fid  uint32
}

func (c *Conn) newFid() uint32 {
	return c.fids.Add(1) - 1
}

func splitPath(path string) []string {
	var names []string
	for p := range strings.SplitSeq(path, "/") {
		if p != "" {
			names = append(names, p)
		}
	}
	return names
}

// Walk walks the given path from the root and returns a Fid for the final element.
func (c *Conn) Walk(path string) (*Fid, error) {
	return c.walk(splitPath(path))
}

func (c *Conn) walk(names []string) (*Fid, error) {
	newfid := c.newFid()
	rwalk, err := rpc[*proto.RWalk](c, &proto.TWalk{
		Header: proto.Header{Type: proto.Twalk, Tag: 1},
		Fid:    0,
		Newfid: newfid,
		Nwname: uint16(len(names)),
		Wname:  names,
	})
	if err != nil {
		return nil, err
	}
	if len(names) > 0 && int(rwalk.Nwqid) != len(names) {
		return nil, fmt.Errorf("walk returned %d qids, want %d", rwalk.Nwqid, len(names))
	}
	return &Fid{conn: c, fid: newfid}, nil
}

// Open walks to the path and opens the file with the given mode.
func (c *Conn) Open(path string, mode proto.Mode) (*Fid, error) {
	fid, err := c.Walk(path)
	if err != nil {
		return nil, err
	}
	if _, err := rpc[*proto.ROpen](c, &proto.TOpen{
		Header: proto.Header{Type: proto.Topen, Tag: 1},
		Fid:    fid.fid,
		Mode:   mode,
	}); err != nil {
		fid.Clunk()
		return nil, err
	}
	return fid, nil
}

// Create walks to the parent directory and creates a file with the given name.
func (c *Conn) Create(path string, perm uint32, mode proto.Mode) (*Fid, error) {
	names := splitPath(path)
	if len(names) == 0 {
		return nil, errors.New("cannot create root")
	}

	parent, err := c.walk(names[:len(names)-1])
	if err != nil {
		return nil, err
	}

	// Tcreate turns the parent's fid into the new file's.
	if _, err := rpc[*proto.RCreate](c, &proto.TCreate{
		Header: proto.Header{Type: proto.Tcreate, Tag: 1},
		Fid:    parent.fid,
		Name:   names[len(names)-1],
		Perm:   perm,
		Mode:   uint8(mode),
	}); err != nil {
		parent.Clunk()
		return nil, err
	}
	return parent, nil
}

// ReadAt reads up to count bytes at the given offset.
func (f *Fid) ReadAt(offset uint64, count uint32) ([]byte, error) {
	rread, err := rpc[*proto.RRead](f.conn, &proto.TRead{
		Header: proto.Header{Type: proto.Tread, Tag: 1},
		Fid:    f.fid,
		Offset: offset,
		Count:  min(count, f.conn.msize-rreadOverhead),
	})
	if err != nil {
		return nil, err
	}
	return rread.Data, nil
}

// ReadAll reads from offset 0 until an empty Rread.
func (f *Fid) ReadAll() ([]byte, error) {
	var result []byte
	var offset uint64

	for {
		data, err := f.ReadAt(offset, f.conn.msize-rreadOverhead)
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			break
		}
		result = append(result, data...)
		offset += uint64(len(data))
	}

	return result, nil
}

// WriteAt writes data at the given offset, splitting into chunks if necessary.
func (f *Fid) WriteAt(offset uint64, data []byte) error {
	maxChunk := f.conn.msize - twriteOverhead
	for len(data) > 0 {
		chunk := data[:min(uint32(len(data)), maxChunk)]
		rwrite, err := rpc[*proto.RWrite](f.conn, &proto.TWrite{
			Header: proto.Header{Type: proto.Twrite, Tag: 1},
			Fid:    f.fid,
			Offset: offset,
			Count:  uint32(len(chunk)),
			Data:   chunk,
		})
		if err != nil {
			return err
		}
		if rwrite.Count != uint32(len(chunk)) {
			return fmt.Errorf("wrote %d bytes, want %d", rwrite.Count, len(chunk))
		}

		offset += uint64(len(chunk))
		data = data[len(chunk):]
	}

	return nil
}

// Stat returns the stat for this fid.
func (f *Fid) Stat() (proto.Stat, error) {
	rstat, err := rpc[*proto.RStat](f.conn, &proto.TStat{
		Header: proto.Header{Type: proto.Tstat, Tag: 1},
		Fid:    f.fid,
	})
	if err != nil {
		return proto.Stat{}, err
	}
	return rstat.Stat, nil
}

// WStat sets the stat for this fid.
func (f *Fid) WStat(s proto.Stat) error {
	_, err := rpc[*proto.RWstat](f.conn, &proto.TWstat{
		Header: proto.Header{Type: proto.Twstat, Tag: 1},
		Fid:    f.fid,
		Stat:   s,
	})
	return err
}

// Clunk closes this fid.
func (f *Fid) Clunk() error {
	_, err := rpc[*proto.RClunk](f.conn, &proto.TClunk{
		Header: proto.Header{Type: proto.Tclunk, Tag: 1},
		Fid:    f.fid,
	})
	return err
}

// Readdir opens the path as a directory, reads it, and returns the stats.
func (c *Conn) Readdir(path string) ([]proto.Stat, error) {
	fid, err := c.Open(path, proto.Oread)
	if err != nil {
		return nil, err
	}
	defer fid.Clunk()

	data, err := fid.ReadAll()
	if err != nil {
		return nil, err
	}

	return proto.ParseStats(data)
}

// DontTouch returns a Stat with all integer fields at maximum and all strings empty.
// This is the stat that stat(5) calls "don't touch".
func DontTouch() proto.Stat {
	return proto.Stat{
		Type:   ^uint16(0),
		Dev:    ^uint32(0),
		Qid:    proto.Qid{Qtype: ^uint8(0), Vers: ^uint32(0), Uid: ^uint64(0)},
		Mode:   ^uint32(0),
		Atime:  ^uint32(0),
		Mtime:  ^uint32(0),
		Length: ^uint64(0),
		Name:   "",
		Uid:    "",
		Gid:    "",
		Muid:   "",
	}
}
