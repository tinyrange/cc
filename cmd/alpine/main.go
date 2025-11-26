package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"

	"github.com/tinyrange/cc/internal/archive"
)

type File interface {
	io.Reader
	io.ReaderAt
}

type alpinePackage struct {
	files          map[string]archive.Entry
	contentsReader io.ReaderAt
	closer         io.Closer
}

func (p *alpinePackage) Close() error {
	return p.closer.Close()
}

func (p *alpinePackage) Open(filename string) (File, error) {
	ent, ok := p.files[filename]
	if !ok {
		return nil, fmt.Errorf("file %q not found in package", filename)
	}

	r, err := ent.Open(p.contentsReader)
	if err != nil {
		return nil, fmt.Errorf("open file %q in package: %v", filename, err)
	}

	return r, nil
}

func openPackage(base string) (*alpinePackage, error) {
	idx, err := os.Open(base + ".idx")
	if err != nil {
		return nil, fmt.Errorf("open package index %q: %v", base+".idx", err)
	}
	defer idx.Close()

	contents, err := os.Open(base + ".bin")
	if err != nil {
		return nil, fmt.Errorf("open package contents %q: %v", base+".bin", err)
	}

	entries, err := archive.ReadAllEntries(idx)
	if err != nil {
		return nil, fmt.Errorf("create package archive reader: %v", err)
	}

	ret := &alpinePackage{
		files: make(map[string]archive.Entry),
	}

	for _, ent := range entries {
		ret.files[ent.Name] = ent
	}

	ret.contentsReader = contents
	ret.closer = contents

	return ret, nil
}

type alpineDownloader struct {
	Mirror   string
	Version  string
	Arch     string
	Repo     string
	CacheDir string
}

func (d *alpineDownloader) cacheFilePath(cachePath []string) string {
	return filepath.Join(append([]string{d.CacheDir}, cachePath...)...)
}

func (d *alpineDownloader) downloadCached(url string, cachePath []string) (io.ReadCloser, error) {
	// if the cachePath exists, return it
	cacheFile := d.cacheFilePath(cachePath)
	if f, err := os.Open(cacheFile); err == nil {
		return f, nil
	}

	// ensure the cache directory exists
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0755); err != nil {
		return nil, fmt.Errorf("create cache directory for %q: %v", cacheFile, err)
	}

	// download the file
	out, err := os.Create(cacheFile + ".tmp")
	if err != nil {
		return nil, fmt.Errorf("create cache file %q: %v", cacheFile, err)
	}
	defer out.Close()

	slog.Info("Downloading Alpine Linux file",
		"url", url,
		"cacheFile", cacheFile,
	)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download %q: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download %q: status code %d", url, resp.StatusCode)
	}

	if _, err := io.Copy(out, resp.Body); err != nil {
		return nil, fmt.Errorf("save download %q to %q: %v", url, cacheFile, err)
	}

	if err := os.Rename(cacheFile+".tmp", cacheFile); err != nil {
		return nil, fmt.Errorf("rename cache file %q: %v", cacheFile, err)
	}

	if err := out.Close(); err != nil {
		return nil, fmt.Errorf("close cache file %q: %v", cacheFile, err)
	}

	// open the cached file
	f, err := os.Open(cacheFile)
	if err != nil {
		return nil, fmt.Errorf("open cache file %q: %v", cacheFile, err)
	}

	return f, nil
}

func (d *alpineDownloader) convertToPackage(r io.Reader, kind string, cachePath []string) (*alpinePackage, error) {
	// if the cachePath exists, return it
	cacheFile := d.cacheFilePath(cachePath)
	pkg, err := openPackage(cacheFile)
	if err == nil {
		return pkg, nil
	}

	// ensure the cache directory exists
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0755); err != nil {
		return nil, fmt.Errorf("create cache directory for %q: %v", cacheFile, err)
	}

	if kind != "tar.gz" {
		return nil, fmt.Errorf("unsupported package kind %q", kind)
	}

	gzReader, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %v", err)
	}
	defer gzReader.Close()

	reader := tar.NewReader(gzReader)

	idxFile, err := os.Create(cacheFile + ".idx.tmp")
	if err != nil {
		return nil, fmt.Errorf("create package index file %q: %v", cacheFile+".idx.tmp", err)
	}
	defer idxFile.Close()

	binFile, err := os.Create(cacheFile + ".bin")
	if err != nil {
		return nil, fmt.Errorf("create package contents file %q: %v", cacheFile+".bin", err)
	}
	defer binFile.Close()

	ark, err := archive.NewArchiveWriter(idxFile, binFile)
	if err != nil {
		return nil, fmt.Errorf("create package archive writer: %v", err)
	}

	for {
		hdr, err := reader.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("read package tar entry: %v", err)
		}

		ent := &archive.EntryFactory{}

		info := hdr.FileInfo()

		ent.Name(hdr.Name).Mode(info.Mode()).Size(hdr.Size).ModTime(info.ModTime())

		switch hdr.Typeflag {
		case tar.TypeReg:
			ent = ent.Kind(archive.EntryKindRegular)
		case tar.TypeDir:
			ent = ent.Kind(archive.EntryKindDirectory)
		case tar.TypeSymlink:
			ent = ent.Kind(archive.EntryKindSymlink).Linkname(hdr.Linkname)
		default:
			return nil, fmt.Errorf("unsupported tar entry type %q for %q", string(hdr.Typeflag), hdr.Name)
		}

		if err := ark.WriteEntry(ent, reader); err != nil {
			return nil, fmt.Errorf("write package entry %q: %v", hdr.Name, err)
		}
	}

	// rename the index file
	if err := os.Rename(cacheFile+".idx.tmp", cacheFile+".idx"); err != nil {
		return nil, fmt.Errorf("rename package index file %q: %v", cacheFile+".idx", err)
	}

	// close both files
	if err := idxFile.Close(); err != nil {
		return nil, fmt.Errorf("close package index file %q: %v", cacheFile+".idx", err)
	}
	if err := binFile.Close(); err != nil {
		return nil, fmt.Errorf("close package contents file %q: %v", cacheFile+".bin", err)
	}

	// open the package
	pkg, err = openPackage(cacheFile)
	if err != nil {
		return nil, fmt.Errorf("open package %q: %v", cacheFile, err)
	}

	return pkg, nil
}

func (d *alpineDownloader) parseIndex(r io.Reader) (map[string]map[string]string, error) {
	entries := make(map[string]map[string]string)

	scan := bufio.NewScanner(r)

	ent := make(map[string]string)

	for scan.Scan() {
		line := scan.Text()

		if line == "" {
			if name, ok := ent["P"]; ok {
				entries[name] = ent
			}
			ent = make(map[string]string)
			continue
		}

		if len(line) < 2 || line[1] != ':' {
			return nil, fmt.Errorf("unsupported APKINDEX line: %q", line)
		}
		key := line[0:1]
		value := line[2:]

		ent[key] = value
	}

	if err := scan.Err(); err != nil {
		return nil, fmt.Errorf("scan index: %v", err)
	}

	// add the last entry if present
	if len(ent) > 0 {
		if name, ok := ent["P"]; ok {
			entries[name] = ent
		}
	}

	return entries, nil
}

func (d *alpineDownloader) Download(name string) (*alpinePackage, error) {
	indexUrl := fmt.Sprintf("%s/%s/%s/%s/APKINDEX.tar.gz", d.Mirror, d.Version, d.Repo, d.Arch)

	index, err := d.downloadCached(indexUrl, []string{d.Version, d.Repo, d.Arch, "APKINDEX"})
	if err != nil {
		return nil, fmt.Errorf("download APKINDEX: %v", err)
	}
	defer index.Close()

	indexPkg, err := d.convertToPackage(index, "tar.gz", []string{d.Version, d.Repo, d.Arch, "APKINDEX.pkg"})
	if err != nil {
		return nil, fmt.Errorf("parse APKINDEX: %v", err)
	}

	indexFile, err := indexPkg.Open("APKINDEX")
	if err != nil {
		return nil, fmt.Errorf("open APKINDEX tar.gz in package: %v", err)
	}

	idx, err := d.parseIndex(indexFile)
	if err != nil {
		return nil, fmt.Errorf("parse APKINDEX: %v", err)
	}

	pkg, ok := idx[name]
	if !ok {
		return nil, fmt.Errorf("package %q not found in index", name)
	}

	slog.Info("Found Alpine Linux package",
		"package", pkg,
	)

	version := pkg["V"]
	arch := pkg["A"]

	if arch != d.Arch {
		return nil, fmt.Errorf("package %q architecture %q does not match requested architecture %q", name, arch, d.Arch)
	}

	pkgFilename := fmt.Sprintf("%s-%s.apk", name, version)
	pkgUrl := fmt.Sprintf("%s/%s/%s/%s/%s", d.Mirror, d.Version, d.Repo, d.Arch, pkgFilename)

	pkgFile, err := d.downloadCached(pkgUrl, []string{d.Version, d.Repo, d.Arch, "packages", pkgFilename})
	if err != nil {
		return nil, fmt.Errorf("download package %q: %v", name, err)
	}
	defer pkgFile.Close()

	apkPkg, err := d.convertToPackage(pkgFile, "tar.gz", []string{d.Version, d.Repo, d.Arch, "packages", name + "-" + version + ".pkg"})
	if err != nil {
		return nil, fmt.Errorf("convert package %q to internal format: %v", name, err)
	}

	return apkPkg, nil
}

func getAlpineArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return ""
	}
}

func main() {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	mirror := fs.String("mirror", "http://dl-cdn.alpinelinux.org", "Alpine Linux mirror URL")
	version := fs.String("version", "latest-stable", "Alpine Linux version to download")
	arch := fs.String("arch", getAlpineArch(), "Alpine Linux architecture to download")
	repo := fs.String("repo", "main", "Alpine Linux repository to download from")
	cacheDir := fs.String("cache-dir", filepath.Join("local", "alpine"), "Output directory for downloaded files")
	name := fs.String("name", "", "Name of the package to download")
	list := fs.Bool("list", false, "If set, list all files in the package")
	extractFilename := fs.String("extract-filename", "", "If set, extract the following file from the package")
	extractOutput := fs.String("extract-output", "", "If set, write the extracted file to this path")

	if err := fs.Parse(os.Args[1:]); err != nil {
		log.Fatalf("Parse flags: %v", err)
	}

	if *arch == "" {
		log.Fatalf("Unsupported architecture: %s", runtime.GOARCH)
	}

	if *name == "" {
		log.Fatalf("Package name is required")
	}

	dl := &alpineDownloader{
		Mirror:   *mirror,
		Version:  *version,
		Arch:     *arch,
		Repo:     *repo,
		CacheDir: *cacheDir,
	}

	if err := os.MkdirAll(dl.CacheDir, 0755); err != nil {
		log.Fatalf("Create cache directory %q: %v", dl.CacheDir, err)
	}

	pkg, err := dl.Download(*name)
	if err != nil {
		log.Fatalf("Download Alpine Linux package: %v", err)
	}

	if *list {
		for file, ent := range pkg.files {
			fmt.Printf("%s (size: %d, mode: %o)\n", file, ent.Size, ent.Mode)
		}
	}

	if *extractFilename != "" {
		if *extractOutput == "" {
			*extractOutput = path.Base(*extractFilename)
		}

		r, err := pkg.Open(*extractFilename)
		if err != nil {
			log.Fatalf("Open file %q in package: %v", *extractFilename, err)
		}

		outFile, err := os.Create(*extractOutput)
		if err != nil {
			log.Fatalf("Create output file %q: %v", *extractOutput, err)
		}
		defer outFile.Close()

		if _, err := io.Copy(outFile, r); err != nil {
			log.Fatalf("Extract file %q to %q: %v", *extractFilename, *extractOutput, err)
		}
	}
}
