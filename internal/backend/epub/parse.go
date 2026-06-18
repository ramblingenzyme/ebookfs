package epub

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"path"
)

const (
	containerPath = "META-INF/container.xml"
	metadataType  = "application/oebps-package+xml"
)

var (
	ErrMetadata        = errors.New("no metadata file in container")
	ErrContainer       = errors.New("no container file found")
	ErrMetadataMissing = errors.New("could not find metadata file")
)

func getMetadataPath(f *zip.File) (string, error) {
	r, err := f.Open()
	if err != nil {
		return "", err
	}
	defer r.Close()

	var container Container

	d := xml.NewDecoder(r)
	err = d.Decode(&container)
	if err != nil {
		return "", err
	}

	for _, r := range container.Rootfiles {
		if r.MediaType == metadataType {
			return r.FullPath, nil
		}
	}

	return "", ErrMetadata
}

func parsePackage(f *zip.File) (*opfPackage, error) {
	r, err := f.Open()
	if err != nil {
		return &opfPackage{}, err
	}
	defer r.Close()

	var pkg opfPackage

	d := xml.NewDecoder(r)
	err = d.Decode(&pkg)

	return &pkg, err
}

func Parse(bpath string) (*Book, error) {
	// TODO: check if file exists & is a zip file
	r, err := zip.OpenReader(bpath)

	if err != nil {
		return nil, err
	}
	defer r.Close()

	filemap := make(map[string]*zip.File)
	for _, f := range r.File {
		filemap[f.Name] = f
	}

	// TODO: check mimetime

	entry := filemap[containerPath]
	if entry == nil {
		return nil, ErrContainer
	}

	mpath, err := getMetadataPath(entry)
	if err != nil {
		return nil, err
	}

	mfile := filemap[mpath]
	if mfile == nil {
		return nil, ErrMetadataMissing
	}

	pkg, err := parsePackage(mfile)
	pkg.BasePath = path.Dir(mpath)

	if err != nil {
		return nil, err
	}

	return translate(pkg)
}
