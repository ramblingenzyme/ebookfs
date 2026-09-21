package ctl

import (
	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
)

// ctlReadHint is all a read returns. A command's result goes to the log file,
// so there is nothing per-command here.
const ctlReadHint = "write a command line here to run it; read log for results and help for usage.\n"

var newStat = vfile.NewStat

// CtlFile is the root-level "ctl" file. A command line written to it runs on
// close and its outcome goes to the command log.
type CtlFile struct {
	fs.BaseFile
	writes vfile.WriteBuffer
	lib    SearchDeleter
	reg    *registry.BookRegistry
	cmdLog *CommandLog
}

func NewCtlFile(f *fs.FS, lib SearchDeleter, reg *registry.BookRegistry, cmdLog *CommandLog) *CtlFile {
	return &CtlFile{
		BaseFile: *fs.NewBaseFile(newStat(f, "ctl", 0644)),
		writes:   vfile.NewWriteBuffer(4096),
		lib:      lib,
		reg:      reg,
		cmdLog:   cmdLog,
	}
}

func (f *CtlFile) Read(_ uint64, offset uint64, count uint64) ([]byte, error) {
	return vfile.ClampRead([]byte(ctlReadHint), offset, count), nil
}

func (f *CtlFile) Write(fid uint64, offset uint64, data []byte) (uint32, error) {
	return f.writes.Write(fid, offset, data, nil)
}

// Close runs what fid wrote. The result goes to the command log.
func (f *CtlFile) Close(fid uint64) error {
	s := f.writes.TakeText(fid)
	if s == "" {
		return nil
	}
	f.execute(s)
	return nil
}
