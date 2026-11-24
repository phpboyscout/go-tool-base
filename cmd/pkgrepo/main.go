// Command pkgrepo writes the apt and yum indexes for a published release
// (spec 0097 D3), less their signatures. It reads the release's signed
// checksums.txt, the packages it lists and the published indexes, verifies all
// of them, and writes the indexes with the release's packages added.
//
//	go tool pkgrepo --source https://pkg.example.com/acme/tool --tag v1.2.0 --date 2026-01-02T15:04:05Z --keys internal/trustkeys/keys --origin tool [--out dist/pkgrepo]
//
// --source is the store's base, over https, or a directory laid out the same
// way: <tag>/ for each release, apt/ and rpm/ for the indexes.
//
// --keys names a directory of armoured release keys (*.asc). checksums.txt and
// the published indexes must verify against one of them.
//
// --date is the tag's commit date. The indexes carry it, or the published
// index's own when that is later.
//
// --out gets apt/ and rpm/, and base/ with every index file that was read. It
// must be empty or missing.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/signing/verify"
)

const (
	dirMode   = 0o755
	fileMode  = 0o644
	exitUsage = 2
)

type options struct {
	source, tag, date, keys, origin, out string
}

func main() {
	var o options

	flag.StringVar(&o.source, "source", "", "the store's base: an https URL or a directory (required)")
	flag.StringVar(&o.tag, "tag", "", "the release whose packages are added (required)")
	flag.StringVar(&o.date, "date", "", "the tag's commit date, RFC 3339 (required)")
	flag.StringVar(&o.keys, "keys", "", "a directory of armoured release keys, *.asc (required)")
	flag.StringVar(&o.origin, "origin", "", "the repository's name, in apt's Release (required)")
	flag.StringVar(&o.out, "out", filepath.Join("dist", "pkgrepo"), "where the indexes are written")
	flag.Parse()

	for _, v := range []string{o.source, o.tag, o.date, o.keys, o.origin} {
		if v == "" {
			fmt.Fprintln(os.Stderr, "pkgrepo: --source, --tag, --date, --keys and --origin are required")
			os.Exit(exitUsage)
		}
	}

	if err := run(context.Background(), o); err != nil {
		fmt.Fprintf(os.Stderr, "pkgrepo: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, o options) error {
	date, err := time.Parse(time.RFC3339, o.date)
	if err != nil {
		return errors.Wrapf(err, "--date %q", o.date)
	}

	trust, err := releaseKeys(o.keys)
	if err != nil {
		return err
	}

	src, err := newSource(o.source)
	if err != nil {
		return err
	}

	files, err := build(ctx, src, trust, release{tag: o.tag, origin: o.origin, date: date})
	if err != nil {
		return err
	}

	return write(o.out, files)
}

func releaseKeys(dir string) (*verify.TrustSet, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.asc"))
	if err != nil {
		return nil, errors.Wrapf(err, "listing %s", dir)
	}

	keys := make([][]byte, 0, len(names))

	for _, name := range names {
		key, err := os.ReadFile(name)
		if err != nil {
			return nil, errors.Wrapf(err, "reading %s", name)
		}

		keys = append(keys, key)
	}

	trust, err := verify.LoadTrustSet(keys...)

	return trust, errors.Wrapf(err, "the release keys in %s", dir)
}

// write puts files under dir, which must hold nothing: a file left from an
// earlier run would be published with them.
func write(dir string, files map[string][]byte) error {
	if found, err := os.ReadDir(dir); err == nil && len(found) > 0 {
		return errors.Wrapf(errOutput, "%s", dir)
	}

	for name, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))

		if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
			return errors.Wrapf(err, "creating the directory of %s", path)
		}

		if err := os.WriteFile(path, data, fileMode); err != nil {
			return errors.Wrapf(err, "writing %s", path)
		}
	}

	return nil
}
