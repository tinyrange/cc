package alpine

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/tinyrange/cc/internal/archive"
	"github.com/tinyrange/cc/internal/hv"
)

type File interface {
	io.Reader
	io.ReaderAt
	io.Closer
}

// packageFile wraps an archive handle to implement File with a no-op Close.
// Individual files don't need closing because they are section readers into
// the package's underlying file, which is closed via AlpinePackage.Close().
type packageFile struct {
	archive.Handle
}

func (f packageFile) Close() error {
	return nil // No-op: underlying package file handles cleanup
}

type AlpinePackage struct {
	files          map[string]archive.Entry
	contentsReader io.ReaderAt
	closer         io.Closer
}

func (p *AlpinePackage) Close() error {
	return p.closer.Close()
}

func (p *AlpinePackage) Open(filename string) (File, error) {
	ent, ok := p.files[filename]
	if !ok {
		return nil, fmt.Errorf("file %q not found in package", filename)
	}

	r, err := ent.Open(p.contentsReader)
	if err != nil {
		return nil, fmt.Errorf("open file %q in package: %v", filename, err)
	}

	return packageFile{r}, nil
}

func (p *AlpinePackage) ListFiles() []string {
	var files []string
	for name := range p.files {
		files = append(files, name)
	}
	return files
}

func (p *AlpinePackage) Size(filename string) (int64, error) {
	ent, ok := p.files[filename]
	if !ok {
		return 0, fmt.Errorf("file %q not found in package", filename)
	}

	return ent.Size, nil
}

// IsRegularFile returns true if the file exists and is a regular file (not a directory or symlink).
func (p *AlpinePackage) IsRegularFile(filename string) bool {
	ent, ok := p.files[filename]
	if !ok {
		return false
	}
	return ent.Kind == archive.EntryKindRegular
}

// GetEntry returns the archive entry for a file, if it exists.
func (p *AlpinePackage) GetEntry(filename string) (archive.Entry, bool) {
	ent, ok := p.files[filename]
	return ent, ok
}

var ErrPackageExpired = errors.New("package has expired")

// OpenLocalPackage opens an already-downloaded Alpine package from disk.
// Used for offline distribution where packages are bundled with the executable.
// The base path should not include the .idx/.bin extension.
func OpenLocalPackage(base string) (*AlpinePackage, error) {
	return openPackage(base, 0) // No expiration check for local packages
}

func openPackage(base string, maxAge time.Duration) (*AlpinePackage, error) {
	idx, err := os.Open(base + ".idx")
	if err != nil {
		return nil, fmt.Errorf("open package index %q: %v", base+".idx", err)
	}
	defer idx.Close()

	// check age if requested
	if maxAge > 0 {
		info, err := os.Stat(base + ".idx")
		if err != nil {
			return nil, fmt.Errorf("stat package index %q: %v", base+".idx", err)
		}
		if time.Since(info.ModTime()) > maxAge {
			return nil, ErrPackageExpired
		}
	}

	contents, err := os.Open(base + ".bin")
	if err != nil {
		return nil, fmt.Errorf("open package contents %q: %v", base+".bin", err)
	}

	entries, err := archive.ReadAllEntries(idx)
	if err != nil {
		return nil, fmt.Errorf("create package archive reader: %v", err)
	}

	ret := &AlpinePackage{
		files: make(map[string]archive.Entry),
	}

	for _, ent := range entries {
		ret.files[ent.Name] = ent
	}

	ret.contentsReader = contents
	ret.closer = contents

	return ret, nil
}

type AlpineDownloader struct {
	Mirror   string
	Version  string
	Arch     string
	Repo     string
	CacheDir string
}

func (d *AlpineDownloader) cacheFilePath(cachePath []string) string {
	return filepath.Join(append([]string{d.CacheDir}, cachePath...)...)
}

func (d *AlpineDownloader) convertToPackage(r io.Reader, kind string, cachePath []string) (*AlpinePackage, error) {
	cacheFile := d.cacheFilePath(cachePath)

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

	// Close the index file to flush all data
	if err := idxFile.Close(); err != nil {
		return nil, fmt.Errorf("close package index file %q: %v", cacheFile+".idx", err)
	}

	// rename the index file
	if err := os.Rename(cacheFile+".idx.tmp", cacheFile+".idx"); err != nil {
		return nil, fmt.Errorf("rename package index file %q: %v", cacheFile+".idx", err)
	}

	// close both files
	if err := binFile.Close(); err != nil {
		return nil, fmt.Errorf("close package contents file %q: %v", cacheFile+".bin", err)
	}

	// open the package
	pkg, err := openPackage(cacheFile, 0)
	if err != nil {
		return nil, fmt.Errorf("open package %q: %v", cacheFile, err)
	}

	return pkg, nil
}

func (d *AlpineDownloader) downloadAndConvert(url string, kind string, cachePath []string) (*AlpinePackage, error) {
	var openError error

	cacheFile := d.cacheFilePath(cachePath)
	if pkg, err := openPackage(cacheFile, 24*time.Hour); err == nil {
		return pkg, nil
	} else {
		openError = err
	}

	slog.Info("Downloading Alpine Linux file",
		"url", url,
	)

	resp, err := http.Get(url)
	if err != nil {
		if errors.Is(openError, ErrPackageExpired) {
			slog.Warn("could not update package", "error", err)
			return openPackage(cacheFile, 0)
		}
		return nil, fmt.Errorf("download %q: %v", url, err)
	}
	defer resp.Body.Close()

	pb := progressbar.DefaultBytes(
		resp.ContentLength,
		"downloading",
	)
	defer pb.Close()
	resp.Body = io.NopCloser(io.TeeReader(resp.Body, pb))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %q: status code %d", url, resp.StatusCode)
	}

	return d.convertToPackage(resp.Body, kind, cachePath)
}

func (d *AlpineDownloader) parseIndex(r io.Reader) (map[string]map[string]string, error) {
	entries := make(map[string]map[string]string)

	scan := bufio.NewScanner(r)
	// Increase buffer size to handle long dependency lines in community repo
	// Default is 64KB, some packages have very long dependency lists
	scan.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

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

func (d *AlpineDownloader) SetForArchitecture(arch hv.CpuArchitecture, cacheDir string) error {
	if d.Mirror == "" {
		d.Mirror = "https://dl-cdn.alpinelinux.org"
	}
	if d.Version == "" {
		d.Version = "latest-stable"
	}
	if d.Repo == "" {
		d.Repo = "main"
	}
	if d.Arch == "" {
		switch arch {
		case hv.ArchitectureX86_64:
			d.Arch = "x86_64"
		case hv.ArchitectureARM64:
			d.Arch = "aarch64"
		case hv.ArchitectureRISCV64:
			d.Arch = "riscv64"
		default:
			return fmt.Errorf("unsupported architecture for Alpine Linux: %v", arch)
		}
	}
	if d.CacheDir == "" {
		d.CacheDir = cacheDir
	}
	return nil
}

func (d *AlpineDownloader) Download(name string) (*AlpinePackage, error) {
	_, pkg, err := d.downloadWithPath(name)
	return pkg, err
}

// DownloadAndGetPath downloads a package and returns both the package and its cache base path.
// The cache base path can be used to copy the .idx and .bin files for offline distribution.
func (d *AlpineDownloader) DownloadAndGetPath(name string) (cachePath string, pkg *AlpinePackage, err error) {
	return d.downloadWithPath(name)
}

func (d *AlpineDownloader) downloadWithPath(name string) (string, *AlpinePackage, error) {
	indexUrl := fmt.Sprintf("%s/%s/%s/%s/APKINDEX.tar.gz", d.Mirror, d.Version, d.Repo, d.Arch)

	indexCachePath := []string{d.Version, d.Repo, d.Arch, "APKINDEX.pkg"}

	indexPkg, err := d.downloadAndConvert(indexUrl, "tar.gz", indexCachePath)
	if err != nil {
		return "", nil, fmt.Errorf("parse APKINDEX: %v", err)
	}

	indexFile, err := indexPkg.Open("APKINDEX")
	if err != nil {
		return "", nil, fmt.Errorf("open APKINDEX tar.gz in package: %v", err)
	}

	idx, err := d.parseIndex(indexFile)
	if err != nil {
		return "", nil, fmt.Errorf("parse APKINDEX: %v", err)
	}

	pkg, ok := idx[name]
	if !ok {
		return "", nil, fmt.Errorf("package %q not found in index", name)
	}

	version := pkg["V"]
	arch := pkg["A"]

	if arch != d.Arch {
		return "", nil, fmt.Errorf("package %q architecture %q does not match requested architecture %q", name, arch, d.Arch)
	}

	pkgFilename := fmt.Sprintf("%s-%s.apk", name, version)
	pkgUrl := fmt.Sprintf("%s/%s/%s/%s/%s", d.Mirror, d.Version, d.Repo, d.Arch, pkgFilename)

	pkgCachePath := []string{d.Version, d.Repo, d.Arch, "packages", name + "-" + version + ".pkg"}
	apkPkg, err := d.downloadAndConvert(pkgUrl, "tar.gz", pkgCachePath)
	if err != nil {
		return "", nil, fmt.Errorf("convert package %q to internal format: %v", name, err)
	}

	return d.cacheFilePath(pkgCachePath), apkPkg, nil
}
