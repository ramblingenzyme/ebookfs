package fs

import (
	"context"
	"fmt"
	"net"
	pathpkg "path"
	"slices"
	"strings"
	"testing"

	"github.com/knusbaum/go9p"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testing/ninep"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

var (
	makeBook = util.MakeMutableBook
	errTest  = util.ErrTest
)

// openLibrary returns a library in a temporary directory, and that library's
// root.
func openLibrary(t *testing.T) (*library.Library, string) {
	t.Helper()
	cfg := library.Config(util.TestConfig(t))
	lib, err := library.Open(cfg)
	if err != nil {
		t.Fatalf("Open library: %v", err)
	}
	t.Cleanup(func() { lib.Close() })
	return lib, cfg.Root
}

type harness struct {
	t    *testing.T
	lib  *library.Library
	root string
	c    *ninep.Conn
}

func newHarness(t *testing.T, seeds ...func(*testing.T, *library.Library)) *harness {
	t.Helper()
	lib, root := openLibrary(t)
	for _, s := range seeds {
		s(t, lib)
	}
	srv, err := New(lib, Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	server, conn := net.Pipe()
	served := make(chan struct{})
	go func() {
		go9p.ServeReadWriter(server, server, srv.ebookfs.Server())
		close(served)
	}()
	c, err := ninep.Dial(conn, "glenda")
	if err != nil {
		conn.Close()
		<-served
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() {
		conn.Close()
		<-served
		srv.Shutdown(context.Background())
	})
	return &harness{t: t, lib: lib, root: root, c: c}
}

func ingest(title string, authors ...string) func(*testing.T, *library.Library) {
	return func(t *testing.T, lib *library.Library) {
		t.Helper()
		data := util.BuildTestEpub(t, title, authors...)
		h, err := lib.CreateIngest()
		if err != nil {
			t.Fatalf("CreateIngest: %v", err)
		}
		if _, err := h.WriteAt(data, 0); err != nil {
			t.Fatalf("WriteAt: %v", err)
		}
		if _, err := h.Ingest(); err != nil {
			t.Fatalf("Ingest: %v", err)
		}
	}
}

func edited(id int64, e library.Edits) func(*testing.T, *library.Library) {
	return func(t *testing.T, lib *library.Library) {
		t.Helper()
		if _, err := lib.Edit(id, e); err != nil {
			t.Fatalf("Edit(%d): %v", id, err)
		}
	}
}

func (h *harness) upload(name string, epub []byte) error {
	f, err := h.c.Create("/inbox/"+name, 0644, proto.Owrite)
	if err != nil {
		return err
	}
	if err := f.WriteAt(0, epub); err != nil {
		f.Clunk()
		return err
	}
	return f.Clunk()
}

func (h *harness) read(path string) string {
	h.t.Helper()
	return string(h.readBytes(path))
}

// write opens with Otrunc, which Linux v9fs never sends. writeLikeLinux is the
// kernel's sequence.
func (h *harness) write(path, s string) error {
	f, err := h.c.Open(path, proto.Owrite|proto.Otrunc)
	if err != nil {
		return err
	}
	if err := f.WriteAt(0, []byte(s)); err != nil {
		f.Clunk()
		return err
	}
	return f.Clunk()
}

func (h *harness) mustWrite(path, s string) {
	h.t.Helper()
	if err := h.write(path, s); err != nil {
		h.t.Fatalf("write %s: %v", path, err)
	}
}

func (h *harness) ls(path string) []string {
	stats, err := h.c.Readdir(path)
	if err != nil {
		h.t.Fatalf("Readdir %s: %v", path, err)
	}
	var names []string
	for _, s := range stats {
		names = append(names, s.Name)
	}
	slices.Sort(names)
	return names
}

// tree lists every path under path, sorted, since directories list in map
// order.
func (h *harness) tree(path string) []string {
	stats, err := h.c.Readdir(path)
	if err != nil {
		h.t.Fatalf("Readdir %s: %v", path, err)
	}
	var out []string
	for _, s := range stats {
		full := strings.TrimSuffix(path, "/") + "/" + s.Name
		out = append(out, full)
		if s.Mode&proto.DMDIR != 0 {
			out = append(out, h.tree(full)...)
		}
	}
	slices.Sort(out)
	return out
}

// ctl runs cmd and returns its result from /log. The clunk runs the command,
// so the log already holds it when the write returns. A result can span
// lines, so it runs from the newest entry to the end of the log.
func (h *harness) ctl(cmd string) string {
	if err := h.write("/ctl", cmd+"\n"); err != nil {
		h.t.Fatalf("Write /ctl: %v", err)
	}
	log := h.read("/log")
	entry := fmt.Sprintf("  %-30s  ", cmd)
	i := strings.LastIndex(log, entry)
	if i < 0 {
		h.t.Fatalf("/log has no entry for %q:\n%s", cmd, log)
	}
	return strings.TrimSuffix(log[i+len(entry):], "\n")
}

func (h *harness) mustCtl(cmd string) {
	h.t.Helper()
	if result := h.ctl(cmd); !strings.HasPrefix(result, "ok:") {
		h.t.Fatalf("ctl %q = %q, want ok:", cmd, result)
	}
}

// entry returns the last element's stat from its parent's listing, which walks
// fresh every time.
func (h *harness) entry(path string) (proto.Stat, bool) {
	parent, name := pathpkg.Split(path)
	stats, err := h.c.Readdir(parent)
	if err != nil {
		h.t.Fatalf("Readdir %s: %v", parent, err)
	}
	for _, s := range stats {
		if s.Name == name {
			return s, true
		}
	}
	return proto.Stat{}, false
}

func (h *harness) mustExist(path string) {
	h.t.Helper()
	if _, ok := h.entry(path); !ok {
		h.t.Errorf("%s does not exist", path)
	}
}

func (h *harness) mustNotExist(path string) {
	h.t.Helper()
	if _, ok := h.entry(path); ok {
		h.t.Errorf("%s exists but should not", path)
	}
}

func (h *harness) readBytes(path string) []byte {
	h.t.Helper()
	f, err := h.c.Open(path, proto.Oread)
	if err != nil {
		h.t.Fatalf("Open %s: %v", path, err)
	}
	data, err := f.ReadAll()
	if err != nil {
		f.Clunk()
		h.t.Fatalf("ReadAll %s: %v", path, err)
	}
	if err := f.Clunk(); err != nil {
		h.t.Fatalf("Clunk %s: %v", path, err)
	}
	return data
}

func (h *harness) stat(path string) proto.Stat {
	h.t.Helper()
	s, ok := h.entry(path)
	if !ok {
		h.t.Fatalf("%s does not exist", path)
	}
	return s
}

// writeLikeLinux sends what Linux v9fs sends for a > redirect on an existing
// file: an open without Otrunc, a Twstat of length 0 on a second fid, then the
// write.
func (h *harness) writeLikeLinux(path, s string) error {
	f, err := h.c.Open(path, proto.Owrite)
	if err != nil {
		return err
	}
	trunc, err := h.c.Walk(path)
	if err != nil {
		f.Clunk()
		return err
	}
	st := ninep.DontTouch()
	st.Length = 0
	err = trunc.WStat(st)
	if cerr := trunc.Clunk(); err == nil {
		err = cerr
	}
	if err != nil {
		f.Clunk()
		return err
	}
	if err := f.WriteAt(0, []byte(s)); err != nil {
		f.Clunk()
		return err
	}
	return f.Clunk()
}

// search allocates a handle through /search/clone, writes query to its ctl,
// and returns the handle's directory.
func (h *harness) search(query string) string {
	path := "/search/" + strings.TrimSpace(h.read("/search/clone"))
	if err := h.write(path+"/ctl", query); err != nil {
		h.t.Fatalf("write %s/ctl: %v", path, err)
	}
	return path
}
