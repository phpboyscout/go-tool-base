package main

import (
	"cmp"
	"slices"

	"golang.org/x/mod/semver"

	"gitlab.com/phpboyscout/go/errors"
)

// entry is one package in an index. body is its text in the index, kept as it
// was published when the package is already there.
type entry struct {
	name, version, arch, sha string
	body                     string
	// filelists and other are the yum index's second and third files.
	filelists, other string
}

func (e entry) complete() error {
	if e.name == "" || e.arch == "" || e.sha == "" || !semver.IsValid("v"+e.version) {
		return errors.Wrapf(errPackage, "name %q, version %q, architecture %q", e.name, e.version, e.arch)
	}

	return nil
}

type entryKey struct{ name, version, arch string }

func (e entry) key() entryKey { return entryKey{e.name, e.version, e.arch} }

// merge adds entries to the ones an index has, oldest release first. A
// published version is never redefined.
func merge(published, added []entry) ([]entry, error) {
	have := map[entryKey]string{}

	var out []entry

	for _, e := range slices.Concat(published, added) {
		sha, ok := have[e.key()]

		switch {
		case !ok:
			have[e.key()] = e.sha
			out = append(out, e)
		case sha != e.sha:
			return nil, errors.Wrapf(errRedefined, "%s %s for %s", e.name, e.version, e.arch)
		}
	}

	slices.SortFunc(out, func(a, b entry) int {
		return cmp.Or(semver.Compare("v"+a.version, "v"+b.version), cmp.Compare(a.arch, b.arch), cmp.Compare(a.name, b.name))
	})

	return out, nil
}
