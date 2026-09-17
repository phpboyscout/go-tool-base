package gomod

import "runtime/debug"

// VersionSource answers which version of a module the generating binary
// knows, if any.
type VersionSource interface {
	Version(path string) (string, bool)
}

// MapSource is a VersionSource over a map, for tests and for callers that
// already hold the answer.
type MapSource map[string]string

// Version implements VersionSource.
func (m MapSource) Version(path string) (string, bool) {
	v, ok := m[path]

	return v, ok
}

// BuildInfoSource reads the running binary's build info: gtb links every
// adapter, so the version it was built and tested against is the one to
// seed (D4). A binary built without module information answers nothing.
func BuildInfoSource() VersionSource {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return MapSource{}
	}

	m := make(MapSource, len(info.Deps))

	for _, dep := range info.Deps {
		if dep.Replace != nil {
			dep = dep.Replace
		}

		if dep.Version != "" && dep.Version != "(devel)" {
			m[dep.Path] = dep.Version
		}
	}

	return m
}

// Floors is the compatibility baseline this gtb declares: the minimum version
// of a module a scaffold must hold to work with the framework this gtb
// generates for (D8). Edit it in the change that bumps the dependency it
// protects. A present line below its floor is raised on regenerate.
var Floors = map[string]string{}

// Requirements resolves the version of each wanted module in D4's order: a
// declared floor, else the source's answer, else Latest.
func Requirements(paths []string, src VersionSource, floors map[string]string) []Requirement {
	out := make([]Requirement, 0, len(paths))

	for _, path := range paths {
		if v, ok := floors[path]; ok {
			out = append(out, Requirement{Path: path, Version: v, Floor: true})

			continue
		}

		if v, ok := src.Version(path); ok {
			out = append(out, Requirement{Path: path, Version: v})

			continue
		}

		out = append(out, Requirement{Path: path, Version: Latest})
	}

	return out
}
