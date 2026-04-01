package docs

import (
	"bufio"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"
)

// NavNode represents a node in the documentation navigation tree.
type NavNode struct {
	Title    string
	Path     string
	Children []NavNode
}

// --- MkDocs Parsing Logic ---

// MkDocsConfig is the parsed structure of mkdocs.yml for navigation extraction.
type MkDocsConfig struct {
	Nav []any `yaml:"nav"`
}

func parseMkDocsNav(fsys fs.FS) ([]NavNode, error) {
	file, err := fs.ReadFile(fsys, "mkdocs.yml")
	if err == nil {
		var config MkDocsConfig
		if err := yaml.Unmarshal(file, &config); err == nil && len(config.Nav) > 0 {
			return parseNavList(fsys, config.Nav), nil
		}
	}

	// Fallback: Walk FS if mkdocs.yml missing, invalid, or has no nav
	return generateNavFromFS(fsys, ".")
}

func generateNavFromFS(fsys fs.FS, root string) ([]NavNode, error) {
	var nodes []NavNode

	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read docs directory")
	}

	sortEntries(entries)

	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}

		path := filepath.ToSlash(filepath.Join(root, name))

		if entry.IsDir() {
			children, err := generateNavFromFS(fsys, path)
			if err == nil && len(children) > 0 {
				nodes = append(nodes, NavNode{
					Title:    formatTitle(name),
					Children: children,
				})
			}
		} else if strings.HasSuffix(name, ".md") {
			title := extractTitle(fsys, path)
			if title == "" {
				title = formatTitle(strings.TrimSuffix(name, ".md"))
			}

			nodes = append(nodes, NavNode{
				Title: title,
				Path:  path,
			})
		}
	}

	return nodes, nil
}

func sortEntries(entries []fs.DirEntry) {
	sort.Slice(entries, func(i, j int) bool {
		n1, n2 := entries[i].Name(), entries[j].Name()
		isDir1, isDir2 := entries[i].IsDir(), entries[j].IsDir()

		// index.md always first
		if n1 == "index.md" {
			return true
		}

		if n2 == "index.md" {
			return false
		}

		// Files before directories
		if !isDir1 && isDir2 {
			return true
		}

		if isDir1 && !isDir2 {
			return false
		}

		// Alphabetical
		return n1 < n2
	})
}

func formatTitle(name string) string {
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")

	return cases.Title(language.English).String(name)
}

func parseNavList(fsys fs.FS, rawList []any) []NavNode {
	var nodes []NavNode

	for _, item := range rawList {
		switch v := item.(type) {
		case string:
			// "page.md" -> Title is filename, Path is string. Try to extract H1.
			title := extractTitle(fsys, v)
			if title == "" {
				title = v
			}

			nodes = append(nodes, NavNode{Title: title, Path: v})
		case map[string]any:
			// "Title": "page.md" OR "Title": [ ... ]
			for k, val := range v {
				node := NavNode{Title: k}

				switch child := val.(type) {
				case string:
					node.Path = child
				case []any:
					node.Children = parseNavList(fsys, child)
				}

				nodes = append(nodes, node)
			}
		}
	}

	return nodes
}

func extractTitle(fsys fs.FS, path string) string {
	f, err := fsys.Open(path)
	if err != nil {
		return ""
	}

	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if after, ok := strings.CutPrefix(line, "# "); ok {
			return strings.TrimSpace(after)
		}
	}

	return ""
}
