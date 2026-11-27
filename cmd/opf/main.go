// Command opf prints an epub's raw package document (its OPF) to stdout, so it
// can be piped into xmllint or inspected directly. It is a dev/operator tool
// and is not shipped in the Docker image.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ramblingenzyme/ebookfs/pkg/epub"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: %s <book.epub>\n", filepath.Base(os.Args[0]))
		flag.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	if err := run(flag.Arg(0)); err != nil {
		fmt.Fprintf(os.Stderr, "opf: %v\n", err)
		os.Exit(1)
	}
}

// run reads the package document without parsing it: OpenFile locates the OPF
// path and ReadEntry returns its bytes verbatim.
func run(path string) error {
	f, err := epub.OpenFile(path)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := f.ReadEntry(f.PackagePath())
	if err != nil {
		return err
	}

	_, err = os.Stdout.Write(data)

	return err
}
