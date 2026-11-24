package main

import (
	"bytes"
	"encoding/binary"
	"path"
	"strconv"
	"strings"

	"gitlab.com/phpboyscout/go/errors"
)

// The rpm file format: a lead, a signature header padded to eight bytes, and
// the header createrepo reads.
const (
	rpmLeadMagic     = "\xed\xab\xee\xdb"
	rpmLeadSize      = 96
	rpmHeaderMagic   = "\x8e\xad\xe8\x01"
	rpmHeaderPrefix  = 16
	rpmIndexEntry    = 16
	rpmHeaderPadding = 8
)

// rpmNumberWidths are the number types gtb's tags use, in bytes.
var rpmNumberWidths = map[int]int{rpmTypeInt16: binary.Size(uint16(0)), rpmTypeInt32: binary.Size(uint32(0))}

const (
	rpmTypeInt16  = 3
	rpmTypeInt32  = 4
	rpmTypeString = 6
	rpmTypeArray  = 8
	rpmTypeI18n   = 9
)

const (
	tagName        = 1000
	tagVersion     = 1001
	tagRelease     = 1002
	tagEpoch       = 1003
	tagSummary     = 1004
	tagDescription = 1005
	tagBuildTime   = 1006
	tagBuildHost   = 1007
	tagSize        = 1009
	tagVendor      = 1011
	tagLicense     = 1014
	tagPackager    = 1015
	tagGroup       = 1016
	tagURL         = 1020
	tagArch        = 1022
	tagFileModes   = 1030
	tagFileFlags   = 1037
	tagSourceRPM   = 1044
	tagArchiveSize = 1046
	tagDirIndexes  = 1116
	tagBaseNames   = 1117
	tagDirNames    = 1118

	sigTagPayloadSize = 1007
)

// rpmDependencies are the header's dependency lists, in createrepo's order.
var rpmDependencies = []struct {
	element               string
	names, flags, version int
}{
	{"provides", 1047, 1112, 1113},
	{"requires", 1049, 1048, 1050},
	{"conflicts", 1054, 1053, 1055},
	{"obsoletes", 1090, 1114, 1115},
}

const (
	rpmSenseLess    = 1 << 1
	rpmSenseGreater = 1 << 2
	rpmSenseEqual   = 1 << 3
	rpmSenseMask    = rpmSenseLess | rpmSenseGreater | rpmSenseEqual

	fileModeType = 0o170000
	fileModeDir  = 0o040000
	fileFlagHost = 1 << 6
)

var rpmComparisons = map[uint32]string{
	rpmSenseLess:                    "LT",
	rpmSenseGreater:                 "GT",
	rpmSenseEqual:                   "EQ",
	rpmSenseLess | rpmSenseEqual:    "LE",
	rpmSenseGreater | rpmSenseEqual: "GE",
}

// rpmHeader is one header's tags, undecoded.
type rpmHeader map[int]rpmTag

type rpmTag struct {
	kind, count int
	data        []byte
}

type rpmPackage struct {
	header, signature rpmHeader
	// start and end are the header's byte range, which dnf fetches alone.
	start, end int
	size       int
	sha        string
}

type rpmFile struct {
	path       string
	dir, ghost bool
}

func readRPM(data []byte) (rpmPackage, error) {
	if len(data) < rpmLeadSize || !bytes.HasPrefix(data, []byte(rpmLeadMagic)) {
		return rpmPackage{}, errors.Wrap(errPackage, "not an rpm")
	}

	signature, end, err := readRPMHeader(data, rpmLeadSize)
	if err != nil {
		return rpmPackage{}, err
	}

	start := (end + rpmHeaderPadding - 1) / rpmHeaderPadding * rpmHeaderPadding

	header, end, err := readRPMHeader(data, start)
	if err != nil {
		return rpmPackage{}, err
	}

	return rpmPackage{
		header: header, signature: signature, start: start, end: end, size: len(data), sha: sha256Hex(data),
	}, nil
}

// readRPMHeader reads the header at offset at, and where it ends.
func readRPMHeader(data []byte, at int) (rpmHeader, int, error) {
	errTruncated := errors.Wrap(errPackage, "an rpm header is not well formed")

	if at+rpmHeaderPrefix > len(data) || !bytes.HasPrefix(data[at:], []byte(rpmHeaderMagic)) {
		return nil, 0, errTruncated
	}

	count := int(binary.BigEndian.Uint32(data[at+8:]))
	size := int(binary.BigEndian.Uint32(data[at+12:]))
	index := at + rpmHeaderPrefix

	if count > len(data)/rpmIndexEntry || size > len(data) {
		return nil, 0, errTruncated
	}

	store := index + count*rpmIndexEntry
	end := store + size

	if end > len(data) {
		return nil, 0, errTruncated
	}

	header := rpmHeader{}

	for i := range count {
		e := data[index+i*rpmIndexEntry:]
		offset := int(binary.BigEndian.Uint32(e[8:]))

		if offset > size {
			return nil, 0, errTruncated
		}

		header[int(binary.BigEndian.Uint32(e))] = rpmTag{
			kind:  int(binary.BigEndian.Uint32(e[4:])),
			count: int(binary.BigEndian.Uint32(e[12:])),
			data:  data[store+offset : end],
		}
	}

	return header, end, nil
}

// texts is a tag's strings, or none when the header does not carry it.
func (h rpmHeader) texts(tag int) ([]string, error) {
	t, ok := h[tag]
	if !ok {
		return nil, nil
	}

	if (t.kind != rpmTypeString && t.kind != rpmTypeArray && t.kind != rpmTypeI18n) || t.count > len(t.data) {
		return nil, errors.Wrapf(errPackage, "rpm tag %d is not text", tag)
	}

	out := make([]string, 0, t.count)
	rest := t.data

	for range t.count {
		s, after, ok := bytes.Cut(rest, []byte{0})
		if !ok {
			return nil, errors.Wrapf(errPackage, "rpm tag %d is truncated", tag)
		}

		out, rest = append(out, string(s)), after
	}

	return out, nil
}

// numbers is a tag's integers, or none when the header does not carry it.
func (h rpmHeader) numbers(tag int) ([]uint32, error) {
	t, ok := h[tag]
	if !ok {
		return nil, nil
	}

	width := rpmNumberWidths[t.kind]
	if width == 0 || t.count > len(t.data)/width {
		return nil, errors.Wrapf(errPackage, "rpm tag %d is not a list of numbers", tag)
	}

	out := make([]uint32, t.count)
	for i := range out {
		if t.kind == rpmTypeInt16 {
			out[i] = uint32(binary.BigEndian.Uint16(t.data[i*width:]))
		} else {
			out[i] = binary.BigEndian.Uint32(t.data[i*width:])
		}
	}

	return out, nil
}

// rpmReader reads tags and keeps the first error, so a caller reads a package
// in one go and checks once.
type rpmReader struct {
	err error
}

func (r *rpmReader) text(h rpmHeader, tag int) string {
	all := r.texts(h, tag)
	if len(all) == 0 {
		return ""
	}

	return all[0]
}

func (r *rpmReader) texts(h rpmHeader, tag int) []string {
	all, err := h.texts(tag)
	r.keep(err)

	return all
}

func (r *rpmReader) number(h rpmHeader, tag int) uint32 {
	all := r.numbers(h, tag)
	if len(all) == 0 {
		return 0
	}

	return all[0]
}

func (r *rpmReader) numbers(h rpmHeader, tag int) []uint32 {
	all, err := h.numbers(tag)
	r.keep(err)

	return all
}

func (r *rpmReader) keep(err error) {
	if r.err == nil {
		r.err = err
	}
}

// dependencies are one of rpmDependencies' lists. rpmlib() requirements are
// the rpm format's own, which no repository answers, and are left out.
func (r *rpmReader) dependencies(h rpmHeader, names, flags, versions int) []rpmEntryXML {
	n, f, v := r.texts(h, names), r.numbers(h, flags), r.texts(h, versions)
	if len(n) != len(f) || len(n) != len(v) {
		r.keep(errors.Wrapf(errPackage, "rpm tag %d does not match its flags and versions", names))

		return nil
	}

	var out []rpmEntryXML

	for i, name := range n {
		if strings.HasPrefix(name, "rpmlib(") {
			continue
		}

		e := rpmEntryXML{Name: name}

		if cmp, ok := rpmComparisons[f[i]&rpmSenseMask]; ok {
			e.Flags = cmp
			e.Epoch, e.Ver, e.Rel = splitEVR(v[i])
		}

		out = append(out, e)
	}

	return out
}

func (r *rpmReader) files(h rpmHeader) []rpmFile {
	names, dirs := r.texts(h, tagBaseNames), r.texts(h, tagDirNames)
	index, modes, flags := r.numbers(h, tagDirIndexes), r.numbers(h, tagFileModes), r.numbers(h, tagFileFlags)

	if len(names) != len(index) || len(names) != len(modes) || len(names) != len(flags) {
		r.keep(errors.Wrap(errPackage, "the rpm's file lists do not match"))

		return nil
	}

	files := make([]rpmFile, 0, len(names))

	for i, name := range names {
		if int(index[i]) >= len(dirs) {
			r.keep(errors.Wrapf(errPackage, "%s is in no directory", name))

			return nil
		}

		files = append(files, rpmFile{
			path: path.Join(dirs[index[i]], name),
			dir:  modes[i]&fileModeType == fileModeDir, ghost: flags[i]&fileFlagHost != 0,
		})
	}

	return files
}

// splitEVR is an rpm version's epoch, version and release.
func splitEVR(evr string) (epoch, ver, rel string) {
	epoch, rest, ok := strings.Cut(evr, ":")
	if !ok {
		epoch, rest = "0", evr
	}

	if _, err := strconv.ParseUint(epoch, 10, 32); err != nil {
		epoch, rest = "0", evr
	}

	if i := strings.LastIndex(rest, "-"); i >= 0 {
		return epoch, rest[:i], rest[i+1:]
	}

	return epoch, rest, ""
}
