package cvmfs

import (
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	intsqlite "j5.nz/cc/internal/sqlite"
)

const DefaultMirror = "https://cvmfs.neurodesk.org/cvmfs"

const (
	flagDir          = 1
	flagRegularFile  = 4
	flagSymlink      = 8
	flagChunkedFile  = 64
	flagExternalFile = 128

	linuxSIFMT    = 0o170000
	linuxSIFSOCK  = 0o140000
	linuxSIFLNK   = 0o120000
	linuxSIFREG   = 0o100000
	linuxSIFBLK   = 0o060000
	linuxSIFDIR   = 0o040000
	linuxSIFCHR   = 0o020000
	linuxSIFIFO   = 0o010000
	linuxPermMask = 0o7777
)

var errStopWalk = errors.New("stop walk")

type Target struct {
	Raw       string
	Mirror    string
	Repo      string
	Path      string
	LocalPath string
	Remote    bool
}

type DirEntry struct {
	Name    string
	Mode    fs.FileMode
	Size    int64
	ModTime time.Time
}

type Client struct {
	HTTPClient *http.Client
}

type manifest struct {
	RootCatalogHash string
}

type repository struct {
	client   *Client
	mirror   string
	repo     string
	manifest *manifest
	catalogs map[string]*catalog
}

type catalog struct {
	entries []catalogEntry
	nested  []nestedCatalog
}

type nestedCatalog struct {
	Path string
	Sha1 string
}

type catalogChunk struct {
	Md5Path1 int64
	Md5Path2 int64
	Offset   int64
	Size     int64
	Hash     []byte
}

type catalogEntry struct {
	Md5Path1 int64
	Md5Path2 int64
	Parent1  int64
	Parent2  int64

	Hash    []byte
	Size    int64
	Mode    int64
	Mtime   int64
	MtimeNS int64
	Flags   int64
	Name    string
	Symlink string
	UID     int64
	GID     int64

	FullPath string
	Chunks   []catalogChunk
}

func NewClient() *Client {
	return &Client{HTTPClient: http.DefaultClient}
}

func ParseTarget(raw string) (Target, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Target{}, fmt.Errorf("empty CVMFS target")
	}
	if strings.HasPrefix(raw, "/cvmfs/") {
		clean := path.Clean(raw)
		trimmed := strings.TrimPrefix(clean, "/cvmfs/")
		parts := strings.SplitN(trimmed, "/", 2)
		if len(parts) == 0 || parts[0] == "" {
			return Target{}, fmt.Errorf("invalid mounted CVMFS path %q", raw)
		}
		inner := "/"
		if len(parts) == 2 {
			inner = "/" + strings.TrimPrefix(parts[1], "/")
		}
		return Target{
			Raw:       raw,
			Repo:      parts[0],
			Path:      path.Clean(inner),
			LocalPath: clean,
		}, nil
	}
	if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, ".") {
		return Target{
			Raw:       raw,
			Path:      "/",
			LocalPath: raw,
		}, nil
	}
	if strings.HasPrefix(raw, "cvmfs://") {
		u, err := url.Parse(raw)
		if err != nil {
			return Target{}, err
		}
		if u.Host == "" {
			return Target{}, fmt.Errorf("missing CVMFS repository in %q", raw)
		}
		return Target{
			Raw:    raw,
			Remote: true,
			Mirror: DefaultMirror,
			Repo:   u.Host,
			Path:   cleanRepoPath(u.EscapedPath(), u.Path),
		}, nil
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		u, err := url.Parse(raw)
		if err != nil {
			return Target{}, err
		}
		parts := strings.Split(strings.TrimPrefix(path.Clean(u.Path), "/"), "/")
		for i, part := range parts {
			if part != "cvmfs" || i+1 >= len(parts) {
				continue
			}
			mirrorPath := "/"
			if i > 0 {
				mirrorPath = "/" + strings.Join(parts[:i+1], "/")
			} else {
				mirrorPath = "/cvmfs"
			}
			repo := parts[i+1]
			inner := "/"
			if i+2 < len(parts) {
				inner = "/" + strings.Join(parts[i+2:], "/")
			}
			return Target{
				Raw:    raw,
				Remote: true,
				Mirror: (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: mirrorPath}).String(),
				Repo:   repo,
				Path:   path.Clean(inner),
			}, nil
		}
		return Target{}, fmt.Errorf("expected /cvmfs/<repo>/... in %q", raw)
	}
	return Target{}, fmt.Errorf("unsupported CVMFS target %q", raw)
}

func (c *Client) ReadDir(target string) ([]DirEntry, error) {
	parsed, err := ParseTarget(target)
	if err != nil {
		return nil, err
	}
	if !parsed.Remote {
		return readLocalDir(parsed.LocalPath)
	}
	repo := c.newRepository(parsed)
	return repo.ReadDir(parsed.Path)
}

func (c *Client) ReadFile(target string) ([]byte, error) {
	parsed, err := ParseTarget(target)
	if err != nil {
		return nil, err
	}
	if !parsed.Remote {
		return os.ReadFile(parsed.LocalPath)
	}
	repo := c.newRepository(parsed)
	return repo.ReadFile(parsed.Path)
}

func (c *Client) newRepository(target Target) *repository {
	client := c
	if client == nil {
		client = NewClient()
	}
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &repository{
		client:   &Client{HTTPClient: httpClient},
		mirror:   strings.TrimRight(target.Mirror, "/"),
		repo:     target.Repo,
		catalogs: map[string]*catalog{},
	}
}

func readLocalDir(dir string) ([]DirEntry, error) {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]DirEntry, 0, len(items))
	for _, item := range items {
		info, err := item.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, DirEntry{
			Name:    item.Name(),
			Mode:    info.Mode(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *repository) ReadDir(dirPath string) ([]DirEntry, error) {
	dirPath = path.Clean("/" + strings.TrimPrefix(dirPath, "/"))
	foundDir := dirPath == "/"
	children := map[string]DirEntry{}
	err := r.walkPrefix(dirPath, func(ent catalogEntry) error {
		if ent.FullPath == dirPath && ent.isDir() {
			foundDir = true
			return nil
		}
		if path.Dir(ent.FullPath) != dirPath {
			return nil
		}
		children[ent.Name] = DirEntry{
			Name:    ent.Name,
			Mode:    linuxModeToGo(uint32(ent.Mode)),
			Size:    ent.Size,
			ModTime: time.Unix(ent.Mtime, ent.MtimeNS),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !foundDir {
		return nil, os.ErrNotExist
	}
	out := make([]DirEntry, 0, len(children))
	for _, entry := range children {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *repository) ReadFile(filePath string) ([]byte, error) {
	filePath = path.Clean("/" + strings.TrimPrefix(filePath, "/"))
	var found *catalogEntry
	err := r.walkPrefix(filePath, func(ent catalogEntry) error {
		if ent.FullPath != filePath {
			return nil
		}
		copy := ent
		found = &copy
		return errStopWalk
	})
	if err != nil && !errors.Is(err, errStopWalk) {
		return nil, err
	}
	if found == nil {
		return nil, os.ErrNotExist
	}
	if found.isDir() {
		return nil, fmt.Errorf("%q is a directory", filePath)
	}
	if found.isSymlink() {
		return nil, fmt.Errorf("%q is a symlink", filePath)
	}
	return r.readEntryData(*found)
}

func (r *repository) readEntryData(ent catalogEntry) ([]byte, error) {
	if ent.Flags&flagExternalFile == 0 && !ent.isChunked() {
		return r.fetchDataObject(stringifyHash(ent.Hash), false)
	}
	if len(ent.Chunks) == 0 {
		return nil, fmt.Errorf("chunk metadata missing for %q", ent.FullPath)
	}
	buf := make([]byte, 0, ent.Size)
	for _, chunk := range ent.Chunks {
		data, err := r.fetchDataObject(stringifyHash(chunk.Hash), true)
		if err != nil {
			return nil, err
		}
		buf = append(buf, data...)
	}
	if int64(len(buf)) > ent.Size {
		buf = buf[:ent.Size]
	}
	return buf, nil
}

func (r *repository) walkPrefix(prefix string, visit func(ent catalogEntry) error) error {
	root, err := r.rootCatalog()
	if err != nil {
		return err
	}
	globalPaths := map[string]string{"0:0": "/"}
	return r.walkCatalog(root, "/", prefix, globalPaths, visit)
}

func (r *repository) walkCatalog(cat *catalog, baseParent, prefix string, globalPaths map[string]string, visit func(ent catalogEntry) error) error {
	local := make(map[string]catalogEntry, len(cat.entries))
	for _, ent := range cat.entries {
		local[ent.pathHash()] = ent
	}
	memo := map[string]string{}
	var resolve func(catalogEntry) (string, error)
	resolve = func(ent catalogEntry) (string, error) {
		if full, ok := memo[ent.pathHash()]; ok {
			return full, nil
		}
		parentPath, ok := globalPaths[ent.parentHash()]
		if !ok {
			if ent.parentHash() == "0:0" {
				parentPath = baseParent
			} else if parent, ok := local[ent.parentHash()]; ok {
				full, err := resolve(parent)
				if err != nil {
					return "", err
				}
				parentPath = full
			} else {
				return "", fmt.Errorf("parent not found for %q in repo %q", ent.Name, r.repo)
			}
		}
		full := path.Clean(path.Join(parentPath, ent.Name))
		memo[ent.pathHash()] = full
		globalPaths[ent.pathHash()] = full
		return full, nil
	}
	for i := range cat.entries {
		full, err := resolve(cat.entries[i])
		if err != nil {
			return err
		}
		cat.entries[i].FullPath = full
		if hasCommonFragment(full, prefix) {
			if err := visit(cat.entries[i]); err != nil {
				return err
			}
		}
	}
	for _, nested := range cat.nested {
		nestedPath := path.Clean("/" + strings.TrimPrefix(nested.Path, "/"))
		if !hasCommonFragment(nestedPath, prefix) {
			continue
		}
		child, err := r.openCatalog(nested.Sha1)
		if err != nil {
			return err
		}
		if err := r.walkCatalog(child, path.Dir(nestedPath), prefix, globalPaths, visit); err != nil {
			return err
		}
	}
	return nil
}

func (r *repository) rootCatalog() (*catalog, error) {
	manifest, err := r.getManifest()
	if err != nil {
		return nil, err
	}
	return r.openCatalog(manifest.RootCatalogHash)
}

func (r *repository) getManifest() (*manifest, error) {
	if r.manifest != nil {
		return r.manifest, nil
	}
	resp, err := r.client.HTTPClient.Get(r.mirror + "/" + r.repo + "/.cvmfspublished")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch manifest: unexpected status %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out manifest
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "--" {
			continue
		}
		switch line[0] {
		case 'C':
			out.RootCatalogHash = line[1:]
		}
	}
	if out.RootCatalogHash == "" {
		return nil, fmt.Errorf("manifest missing root catalog hash")
	}
	r.manifest = &out
	return r.manifest, nil
}

func (r *repository) openCatalog(hash string) (*catalog, error) {
	if cat, ok := r.catalogs[hash]; ok {
		return cat, nil
	}
	data, err := r.fetchCatalogDB(hash)
	if err != nil {
		return nil, err
	}
	db, err := intsqlite.ParseDatabase(data)
	if err != nil {
		return nil, err
	}
	cat, err := loadCatalog(db)
	if err != nil {
		return nil, err
	}
	r.catalogs[hash] = cat
	return cat, nil
}

func (r *repository) fetchCatalogDB(hash string) ([]byte, error) {
	resp, err := r.client.HTTPClient.Get(r.objectURL(hash, "C"))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch catalog: unexpected status %s", resp.Status)
	}
	zr, err := zlib.NewReader(resp.Body)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

func (r *repository) fetchDataObject(hash string, partial bool) ([]byte, error) {
	suffix := ""
	if partial {
		suffix = "P"
	}
	resp, err := r.client.HTTPClient.Get(r.objectURL(hash, suffix))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch data object: unexpected status %s", resp.Status)
	}
	zr, err := zlib.NewReader(resp.Body)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

func (r *repository) objectURL(hash, suffix string) string {
	return fmt.Sprintf("%s/%s/data/%s/%s%s", r.mirror, r.repo, hash[:2], hash[2:], suffix)
}

func loadCatalog(db *intsqlite.SQLiteDatabase) (*catalog, error) {
	chunks, err := loadChunks(db)
	if err != nil {
		return nil, err
	}
	entries, err := loadEntries(db, chunks)
	if err != nil {
		return nil, err
	}
	nested, err := loadNestedCatalogs(db)
	if err != nil {
		return nil, err
	}
	return &catalog{entries: entries, nested: nested}, nil
}

func loadEntries(db *intsqlite.SQLiteDatabase, chunks map[string][]catalogChunk) ([]catalogEntry, error) {
	tbl, err := db.Table("catalog")
	if err != nil {
		return nil, err
	}
	var out []catalogEntry
	if err := tbl.Read(func(row []any) error {
		byName := sliceToRowMap([]string{
			"md5path_1", "md5path_2", "parent_1", "parent_2", "hardlinks", "hash", "size", "mode", "mtime", "mtimens", "flags", "name", "symlink", "uid", "gid", "xattr",
		}, row)
		if len(row) == 15 {
			byName = sliceToRowMap([]string{
				"md5path_1", "md5path_2", "parent_1", "parent_2", "hardlinks", "hash", "size", "mode", "mtime", "flags", "name", "symlink", "uid", "gid", "xattr",
			}, row)
		}
		entry := catalogEntry{
			Md5Path1: asInt64(byName["md5path_1"]),
			Md5Path2: asInt64(byName["md5path_2"]),
			Parent1:  asInt64(byName["parent_1"]),
			Parent2:  asInt64(byName["parent_2"]),
			Hash:     asBytes(byName["hash"]),
			Size:     asInt64(byName["size"]),
			Mode:     asInt64(byName["mode"]),
			Mtime:    asInt64(byName["mtime"]),
			MtimeNS:  asInt64(byName["mtimens"]),
			Flags:    asInt64(byName["flags"]),
			Name:     asString(byName["name"]),
			Symlink:  asString(byName["symlink"]),
			UID:      asInt64(byName["uid"]),
			GID:      asInt64(byName["gid"]),
		}
		if entry.Flags&flagChunkedFile != 0 {
			entry.Chunks = chunks[entry.pathHash()]
			sort.Slice(entry.Chunks, func(i, j int) bool { return entry.Chunks[i].Offset < entry.Chunks[j].Offset })
		}
		out = append(out, entry)
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func loadNestedCatalogs(db *intsqlite.SQLiteDatabase) ([]nestedCatalog, error) {
	tbl, err := db.Table("nested_catalogs")
	if _, ok := err.(intsqlite.ErrTableNotFound); ok {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []nestedCatalog
	if err := tbl.Read(func(row []any) error {
		out = append(out, nestedCatalog{
			Path: asString(row[0]),
			Sha1: asString(row[1]),
		})
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func loadChunks(db *intsqlite.SQLiteDatabase) (map[string][]catalogChunk, error) {
	tbl, err := db.Table("chunks")
	if _, ok := err.(intsqlite.ErrTableNotFound); ok {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string][]catalogChunk{}
	if err := tbl.Read(func(row []any) error {
		chunk := catalogChunk{
			Md5Path1: asInt64(row[0]),
			Md5Path2: asInt64(row[1]),
			Offset:   asInt64(row[2]),
			Size:     asInt64(row[3]),
			Hash:     asBytes(row[4]),
		}
		key := fmt.Sprintf("%x:%x", chunk.Md5Path1, chunk.Md5Path2)
		out[key] = append(out[key], chunk)
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func asString(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case []byte:
		return string(val)
	default:
		return fmt.Sprint(val)
	}
}

func asInt64(v any) int64 {
	switch val := v.(type) {
	case nil:
		return 0
	case int64:
		return val
	case int:
		return int64(val)
	case int32:
		return int64(val)
	case int16:
		return int64(val)
	case int8:
		return int64(val)
	case uint64:
		return int64(val)
	case uint32:
		return int64(val)
	case uint16:
		return int64(val)
	case uint8:
		return int64(val)
	default:
		return 0
	}
}

func asBytes(v any) []byte {
	switch val := v.(type) {
	case nil:
		return nil
	case []byte:
		return append([]byte(nil), val...)
	case string:
		return []byte(val)
	default:
		return nil
	}
}

func sliceToRowMap(cols []string, values []any) map[string]any {
	out := make(map[string]any, len(cols))
	for i := range cols {
		if i < len(values) {
			out[cols[i]] = values[i]
		}
	}
	return out
}

func cleanRepoPath(escaped, raw string) string {
	if escaped != "" {
		if unescaped, err := url.PathUnescape(escaped); err == nil {
			return path.Clean("/" + strings.TrimPrefix(unescaped, "/"))
		}
	}
	return path.Clean("/" + strings.TrimPrefix(raw, "/"))
}

func hasCommonFragment(a, b string) bool {
	a = path.Clean("/" + strings.TrimPrefix(a, "/"))
	b = path.Clean("/" + strings.TrimPrefix(b, "/"))
	if strings.HasPrefix(a, b) {
		return true
	}
	return len(b) > len(a) && strings.HasPrefix(b, a+"/")
}

func stringifyHash(hash []byte) string {
	return fmt.Sprintf("%x", hash)
}

func (ent catalogEntry) pathHash() string {
	return fmt.Sprintf("%x:%x", ent.Md5Path1, ent.Md5Path2)
}

func (ent catalogEntry) parentHash() string {
	return fmt.Sprintf("%x:%x", ent.Parent1, ent.Parent2)
}

func (ent catalogEntry) isDir() bool {
	return ent.Flags&flagDir != 0
}

func (ent catalogEntry) isSymlink() bool {
	return ent.Flags&flagSymlink != 0
}

func (ent catalogEntry) isChunked() bool {
	return ent.Flags&flagChunkedFile != 0
}

func linuxModeToGo(mode uint32) fs.FileMode {
	out := fs.FileMode(mode & linuxPermMask)
	switch mode & linuxSIFMT {
	case linuxSIFDIR:
		out |= fs.ModeDir
	case linuxSIFLNK:
		out |= fs.ModeSymlink
	case linuxSIFBLK:
		out |= fs.ModeDevice
	case linuxSIFCHR:
		out |= fs.ModeDevice | fs.ModeCharDevice
	case linuxSIFIFO:
		out |= fs.ModeNamedPipe
	case linuxSIFSOCK:
		out |= fs.ModeSocket
	case linuxSIFREG:
	}
	return out
}
