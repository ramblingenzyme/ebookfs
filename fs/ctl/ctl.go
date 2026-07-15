package ctl

import (
	"strings"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/fs/registry"
	"github.com/ramblingenzyme/ebookfs/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/library"
)

// ctlReadHint is what reading the ctl file returns. ctl is a command sink; a
// command's result (and any error) is recorded in the log file, not echoed back
// here, so there is nothing per-command to read.
const ctlReadHint = "write a command line here to run it; read log for results and help for usage.\n"

// CtlFile is the root-level "ctl" file. Writing a command line executes it on
// close; the outcome is recorded in the command log (read via the log file)
// rather than echoed back. Reading ctl returns a short usage hint.
type CtlFile struct {
	fs.BaseFile
	writes vfile.WriteBuffer
	lib    library.Library
	reg    *registry.BookRegistry
	cmdLog *CommandLog
}

// NewCtlFile creates the root ctl file.
func NewCtlFile(f *fs.FS, lib library.Library, reg *registry.BookRegistry, cmdLog *CommandLog) *CtlFile {
	return &CtlFile{
		BaseFile: *fs.NewBaseFile(vfile.NewStat(f, "ctl", 0644)),
		writes:   vfile.NewWriteBuffer(4096),
		lib:      lib,
		reg:      reg,
		cmdLog:   cmdLog,
	}
}

// Read returns a short usage hint; command results live in the log file.
func (f *CtlFile) Read(fid uint64, offset uint64, count uint64) ([]byte, error) {
	return vfile.ClampRead([]byte(ctlReadHint), offset, count), nil
}

// Write buffers incoming data. The command is processed on Close.
func (f *CtlFile) Write(fid uint64, offset uint64, data []byte) (uint32, error) {
	return f.writes.Write(fid, offset, data, nil)
}

// Close commits the buffered writes from fid and executes the command. The
// result is recorded in the command log rather than returned here.
func (f *CtlFile) Close(fid uint64) error {
	buf := f.writes.Take(fid)
	if buf == nil {
		return nil
	}
	s := strings.TrimSpace(string(buf))
	if s == "" {
		return nil
	}
	execute(s, f.lib, f.reg, f.cmdLog)
	return nil
}
