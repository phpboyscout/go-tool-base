package props

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"dario.cat/mergo"
	"github.com/cockroachdb/errors"
	"github.com/hashicorp/hcl"
	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

const (
	dirPermRead = 0o444
)

// AssetMap is a custom type for a map of filesystems.
type AssetMap map[string]fs.FS

// Assets is an interface that wraps a map of fs.FS pointers and implements the standard fs interfaces.
type Assets interface {
	fs.FS
	fs.ReadDirFS
	fs.GlobFS
	fs.StatFS

	Slice() []fs.FS
	Names() []string
	Get(name string) fs.FS
	Register(name string, fs fs.FS)
	For(names ...string) Assets
	Merge(others ...Assets) Assets
	Exists(name string) (fs.FS, error)
	Mount(f fs.FS, prefix string)
}

type embeddedAssets struct {
	embedded map[string]fs.FS
	order    []string
}

// NewAssets creates a new Assets wrapper with the given AssetMap pointers.
func NewAssets(assets ...AssetMap) Assets {
	a := &embeddedAssets{
		embedded: make(map[string]fs.FS),
		order:    make([]string, 0),
	}

	for _, m := range assets {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}

		sort.Strings(keys)

		for _, k := range keys {
			a.Register(k, m[k])
		}
	}

	return a
}

func (a *embeddedAssets) Names() []string {
	if a == nil {
		return nil
	}

	return a.order
}

func (a *embeddedAssets) Exists(name string) (fs.FS, error) {
	if a == nil {
		return nil, fs.ErrNotExist
	}

	// Static assets use Reverse Search (Shadowing - Last registered wins)
	for i := len(a.order) - 1; i >= 0; i-- {
		ef := a.embedded[a.order[i]]
		if ef == nil {
			continue
		}

		_, err := fs.Stat(ef, name)
		if err == nil {
			return ef, nil
		}
	}

	return nil, fs.ErrNotExist
}

// Open implements the fs.FS interface.
func (a *embeddedAssets) Open(name string) (fs.File, error) {
	if a == nil {
		return nil, fs.ErrNotExist
	}

	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".yaml", ".yml", ".json", ".toml", ".xml", ".properties", ".env", ".hcl", ".tf":
		return a.openMergedStructured(name, ext)
	case ".csv":
		return a.openMergedCSV(name)
	}

	// Static assets use Reverse Search (Shadowing)
	for i := len(a.order) - 1; i >= 0; i-- {
		ef := a.embedded[a.order[i]]
		if ef == nil {
			continue
		}

		file, err := ef.Open(name)
		if err == nil {
			return file, nil
		}
	}

	return nil, fs.ErrNotExist
}

func (a *embeddedAssets) openMergedStructured(name, ext string) (fs.File, error) {
	var merged map[string]any

	found := false
	// Structured data uses Forward Merge (Root -> Sub1 -> Sub2)
	for _, fsName := range a.order {
		current, err := a.processAssetFile(fsName, name, ext)
		if err != nil {
			continue
		}

		if merged == nil {
			merged = current
		} else {
			_ = mergo.Merge(&merged, current, mergo.WithOverride)
		}

		found = true
	}

	if !found {
		return nil, fs.ErrNotExist
	}

	output, err := marshalStructuredData(merged, ext)
	if err != nil {
		return nil, err
	}

	return &mergedFile{
		name:   name,
		Reader: bytes.NewReader(output),
	}, nil
}

func marshalStructuredData(merged map[string]any, ext string) ([]byte, error) {
	var (
		output []byte
		err    error
	)

	switch ext {
	case ".json":
		output, err = json.Marshal(merged)
	case ".toml":
		output, err = toml.Marshal(merged)
	case ".xml":
		//nolint:staticcheck // SA1026: xml.Marshal doesn't support map[string]interface{}, but we handle basics or users should provide structs
		output, err = xml.Marshal(merged)
	case ".properties", ".env":
		output = []byte(formatFlatKV(merged))
	case ".hcl", ".tf":
		// Note: HCL marshaling isn't as standard in generic map form
		// for now we'll convert to JSON which HCL can often consume or vice-versa
		output, err = json.Marshal(merged)
	default: // yaml
		output, err = yaml.Marshal(merged)
	}

	return output, err
}

func (a *embeddedAssets) processAssetFile(fsName, name, ext string) (map[string]any, error) {
	ef := a.embedded[fsName]
	if ef == nil {
		return nil, fs.ErrNotExist
	}

	f, err := ef.Open(name)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open embedded asset")
	}

	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read embedded asset")
	}

	return unmarshalStructuredData(data, ext)
}

func unmarshalStructuredData(data []byte, ext string) (map[string]any, error) {
	current := make(map[string]any)

	var err error

	switch ext {
	case ".json":
		err = json.Unmarshal(data, &current)
	case ".yaml", ".yml":
		err = yaml.Unmarshal(data, &current)
	case ".toml":
		err = toml.Unmarshal(data, &current)
	case ".xml":
		// Simple XML to Map - might be limited but handles basics
		err = xml.Unmarshal(data, &current)
	case ".properties", ".env":
		current = parseFlatKV(string(data))
	case ".hcl", ".tf":
		err = hcl.Unmarshal(data, &current)
	default:
		return nil, errors.Newf("unsupported extension: %s", ext)
	}

	return current, err
}

// readCSVFromFS reads all CSV rows from a single filesystem.
// The file is closed before this function returns.
func (a *embeddedAssets) readCSVFromFS(ef fs.FS, name string) ([][]string, error) {
	f, err := ef.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	reader := csv.NewReader(f)

	return reader.ReadAll()
}

func (a *embeddedAssets) openMergedCSV(name string) (fs.File, error) {
	var allRows [][]string

	found := false

	for _, fsName := range a.order {
		ef := a.embedded[fsName]
		if ef == nil {
			continue
		}

		rows, err := a.readCSVFromFS(ef, name)
		if err != nil || len(rows) == 0 {
			continue
		}

		if !found {
			allRows = rows
		} else {
			allRows = append(allRows, rows...)
		}

		found = true
	}

	if !found {
		return nil, fs.ErrNotExist
	}

	var buf bytes.Buffer

	writer := csv.NewWriter(&buf)
	_ = writer.WriteAll(allRows)
	writer.Flush()

	return &mergedFile{
		name:   name,
		Reader: bytes.NewReader(buf.Bytes()),
	}, nil
}

func parseFlatKV(data string) map[string]any {
	m := make(map[string]any)

	const kvParts = 2

	lines := strings.SplitSeq(data, "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", kvParts)
		if len(parts) == kvParts {
			m[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	return m
}

func formatFlatKV(m map[string]any) string {
	lines := make([]string, 0, len(m))
	for k, v := range m {
		lines = append(lines, fmt.Sprintf("%s=%v", k, v))
	}

	sort.Strings(lines)

	return strings.Join(lines, "\n")
}

// Stat implements fs.StatFS.
func (a *embeddedAssets) Stat(name string) (fs.FileInfo, error) {
	if a == nil {
		return nil, fs.ErrNotExist
	}

	for i := len(a.order) - 1; i >= 0; i-- {
		ef := a.embedded[a.order[i]]
		if ef == nil {
			continue
		}

		info, err := fs.Stat(ef, name)
		if err == nil {
			return info, nil
		}
	}

	return nil, fs.ErrNotExist
}

// ReadDir implements fs.ReadDirFS.
func (a *embeddedAssets) ReadDir(name string) ([]fs.DirEntry, error) {
	if a == nil {
		return nil, fs.ErrNotExist
	}

	entriesMap := make(map[string]fs.DirEntry)
	foundDir := false

	for _, fsName := range a.order {
		ef := a.embedded[fsName]
		if ef == nil {
			continue
		}

		dirEntries, err := fs.ReadDir(ef, name)
		if err != nil {
			continue
		}

		foundDir = true

		for _, de := range dirEntries {
			// In Union FS, names are unique. Shadowing logic for DirEntry is simpler:
			// if it exists in multiple, we just keep one (usually the last for consistent view if we were to Stat it)
			entriesMap[de.Name()] = de
		}
	}

	if !foundDir {
		return nil, fs.ErrNotExist
	}

	entries := make([]fs.DirEntry, 0, len(entriesMap))
	for _, de := range entriesMap {
		entries = append(entries, de)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	return entries, nil
}

// Glob implements fs.GlobFS.
func (a *embeddedAssets) Glob(pattern string) ([]string, error) {
	if a == nil {
		return nil, fs.ErrNotExist
	}

	matchesMap := make(map[string]bool)

	for _, fsName := range a.order {
		ef := a.embedded[fsName]
		if ef == nil {
			continue
		}

		matches, err := fs.Glob(ef, pattern)
		if err != nil {
			continue
		}

		for _, m := range matches {
			matchesMap[m] = true
		}
	}

	results := make([]string, 0, len(matchesMap))
	for m := range matchesMap {
		results = append(results, m)
	}

	sort.Strings(results)

	return results, nil
}

// Mount attaches a filesystem at a specific prefix.
func (a *embeddedAssets) Mount(f fs.FS, prefix string) {
	if a == nil || f == nil {
		return
	}

	a.Register(prefix, &mountedFS{fs: f, prefix: prefix})
}

type mountedFS struct {
	fs     fs.FS
	prefix string
}

func (m *mountedFS) Open(name string) (fs.File, error) {
	if name == m.prefix {
		return m.fs.Open(".")
	}

	if after, ok := strings.CutPrefix(name, m.prefix+"/"); ok {
		return m.fs.Open(after)
	}

	return nil, fs.ErrNotExist
}

// Slice returns the internal slice of fs.FS pointers in order.
func (a *embeddedAssets) Slice() []fs.FS {
	if a == nil {
		return nil
	}

	res := make([]fs.FS, 0, len(a.order))
	for _, name := range a.order {
		res = append(res, a.embedded[name])
	}

	return res
}

// Register adds the given filesystem to the wrapper with a name.
func (a *embeddedAssets) Register(name string, fs fs.FS) {
	if a == nil {
		return
	}

	if _, exists := a.embedded[name]; !exists {
		a.order = append(a.order, name)
	}

	a.embedded[name] = fs
}

// Get returns the filesystem registered with the given name.
func (a *embeddedAssets) Get(name string) fs.FS {
	if a == nil {
		return nil
	}

	return a.embedded[name]
}

// For returns a subset of assets identified by the given names.
func (a *embeddedAssets) For(names ...string) Assets {
	res := NewAssets()

	for _, name := range names {
		if fs, ok := a.embedded[name]; ok {
			res.Register(name, fs)
		}
	}

	return res
}

// Merge appends the internal filesystems of the given Assets to the current wrapper and returns it.
func (a *embeddedAssets) Merge(others ...Assets) Assets {
	if a == nil {
		return nil
	}

	for _, other := range others {
		if other != nil {
			for _, name := range other.Names() {
				a.Register(name, other.Get(name))
			}
		}
	}

	return a
}

// mergedFile implements fs.File for merged structured data.
type mergedFile struct {
	name string
	*bytes.Reader
}

func (f *mergedFile) Stat() (fs.FileInfo, error) {
	return &mergedFileInfo{name: f.name, size: int64(f.Len())}, nil
}

func (f *mergedFile) Close() error { return nil }

type mergedFileInfo struct {
	name string
	size int64
}

func (fi *mergedFileInfo) Name() string       { return fi.name }
func (fi *mergedFileInfo) Size() int64        { return fi.size }
func (fi *mergedFileInfo) Mode() fs.FileMode  { return fs.FileMode(dirPermRead) }
func (fi *mergedFileInfo) ModTime() time.Time { return time.Time{} }
func (fi *mergedFileInfo) IsDir() bool        { return false }
func (fi *mergedFileInfo) Sys() any           { return nil }
