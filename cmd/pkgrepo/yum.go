package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"gitlab.com/phpboyscout/go/errors"
)

const (
	yumDir     = "rpm/"
	repomdFile = "repodata/repomd.xml"
	repomdSig  = "repodata/repomd.xml.asc"
	xmlHeader  = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"
)

// yumData are the files repomd.xml names, in createrepo's order.
var yumData = []struct{ name, root string }{
	{"primary", `metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm"`},
	{"filelists", `filelists xmlns="http://linux.duke.edu/metadata/filelists"`},
	{"other", `otherdata xmlns="http://linux.duke.edu/metadata/other"`},
}

var rpmArches = []string{"x86_64", "aarch64"}

// yumDataFile is a data file's location: content-named, so never replaced.
var yumDataFile = regexp.MustCompile(`^repodata/[0-9a-f]{64}-(primary|filelists|other)\.xml\.gz$`)

// primaryFile is a file primary.xml lists as well as filelists.xml, by
// createrepo's rule.
var primaryFile = regexp.MustCompile(`^(/etc/.*|.*bin/.*|/usr/lib/sendmail)$`)

type repomdXML struct {
	XMLName  xml.Name        `xml:"http://linux.duke.edu/metadata/repo repomd"`
	Revision int64           `xml:"revision"`
	Data     []repomdDataXML `xml:"data"`
}

type repomdDataXML struct {
	Type         string      `xml:"type,attr"`
	Checksum     checksumXML `xml:"checksum"`
	OpenChecksum checksumXML `xml:"open-checksum"`
	Location     locationXML `xml:"location"`
	Timestamp    int64       `xml:"timestamp"`
	Size         int         `xml:"size"`
	OpenSize     int         `xml:"open-size"`
}

type checksumXML struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

type locationXML struct {
	Href string `xml:"href,attr"`
}

type versionXML struct {
	Epoch string `xml:"epoch,attr"`
	Ver   string `xml:"ver,attr"`
	Rel   string `xml:"rel,attr"`
}

type rpmEntryXML struct {
	Name  string `xml:"name,attr"`
	Flags string `xml:"flags,attr,omitempty"`
	Epoch string `xml:"epoch,attr,omitempty"`
	Ver   string `xml:"ver,attr,omitempty"`
	Rel   string `xml:"rel,attr,omitempty"`
}

// publishedXML is a published package in any of the three files, with its text
// kept whole.
type publishedXML struct {
	PkgID   string      `xml:"pkgid,attr"`
	Name    string      `xml:"name"`
	Arch    string      `xml:"arch"`
	Version versionXML  `xml:"version"`
	Sum     checksumXML `xml:"checksum"`
	Inner   string      `xml:",innerxml"`
}

type yumIndex struct {
	entries []entry
	date    time.Time
}

func (r *reader) yumIndex(ctx context.Context) (yumIndex, error) {
	raw, err := r.signed(ctx, yumDir+repomdFile, yumDir+repomdSig)
	if err != nil || raw == nil {
		return yumIndex{}, err
	}

	var repomd repomdXML
	if err := xml.Unmarshal(raw, &repomd); err != nil {
		return yumIndex{}, errors.Wrapf(errIndex, "repomd.xml: %v", err)
	}

	files := map[string][]publishedXML{}

	for _, d := range repomd.Data {
		if _, dup := files[d.Type]; dup || !yumDataFile.MatchString(d.Location.Href) || !strings.HasSuffix(d.Location.Href, "-"+d.Type+".xml.gz") {
			return yumIndex{}, errors.Wrapf(errIndex, "repomd.xml names %q as %q", d.Location.Href, d.Type)
		}

		files[d.Type], err = r.yumData(ctx, d)
		if err != nil {
			return yumIndex{}, err
		}
	}

	if len(files) != len(yumData) {
		return yumIndex{}, errors.Wrap(errIndex, "repomd.xml does not name primary, filelists and other")
	}

	entries, err := publishedRPMs(files)

	return yumIndex{entries: entries, date: time.Unix(repomd.Revision, 0)}, err
}

func (r *reader) yumData(ctx context.Context, d repomdDataXML) ([]publishedXML, error) {
	zipped, err := r.index(ctx, yumDir+d.Location.Href)
	if err != nil {
		return nil, err
	}

	if d.Checksum.Type != "sha256" || d.Checksum.Value != sha256Hex(zipped) {
		return nil, errors.Wrapf(errUnverified, "%s does not match its repomd.xml", d.Location.Href)
	}

	plain, err := r.unzip(zipped)
	if err != nil {
		return nil, errors.Wrap(err, d.Location.Href)
	}

	var doc struct {
		Packages []publishedXML `xml:"package"`
	}

	if err := xml.Unmarshal(plain, &doc); err != nil {
		return nil, errors.Wrapf(errIndex, "%s: %v", d.Location.Href, err)
	}

	return doc.Packages, nil
}

// publishedRPMs joins the three files' packages on the package's digest.
func publishedRPMs(files map[string][]publishedXML) ([]entry, error) {
	second := map[string]map[string]string{}

	for _, name := range []string{"filelists", "other"} {
		second[name] = map[string]string{}
		for _, p := range files[name] {
			second[name][p.PkgID] = p.Inner
		}
	}

	for _, name := range []string{"filelists", "other"} {
		if len(files[name]) != len(files["primary"]) {
			return nil, errors.Wrapf(errIndex, "primary lists %d packages and %s %d", len(files["primary"]), name, len(files[name]))
		}
	}

	out := make([]entry, 0, len(files["primary"]))

	for _, p := range files["primary"] {
		sha := p.Sum.Value
		lists, haveLists := second["filelists"][sha]
		other, haveOther := second["other"][sha]

		if !haveLists || !haveOther {
			return nil, errors.Wrapf(errIndex, "primary, filelists and other do not list the same packages (%s)", p.Name)
		}

		e := entry{name: p.Name, version: p.Version.Ver, arch: p.Arch, sha: sha, body: p.Inner, filelists: lists, other: other}
		if err := e.complete(); err != nil {
			return nil, errors.Wrap(err, "primary.xml")
		}

		out = append(out, e)
	}

	return out, nil
}

// rpmEntry is the index entry for the rpm at file, under its tag's directory.
func rpmEntry(tag, file string, data []byte) (entry, error) {
	e, v, err := describeRPM(location(tag, file), data)
	if err != nil {
		return entry{}, errors.Wrap(err, file)
	}

	if err := e.complete(); err != nil {
		return entry{}, errors.Wrap(err, file)
	}

	// The release repository has one build of each version.
	if v.Epoch != "0" || v.Rel != "1" {
		return entry{}, errors.Wrapf(errPackage, "%s is epoch %s, release %s", file, v.Epoch, v.Rel)
	}

	if !slices.Contains(rpmArches, e.arch) {
		return entry{}, errors.Wrapf(errPackage, "%s is for %s", file, e.arch)
	}

	return e, agree(tag, file, e, fmt.Sprintf("%s-%s-%s.%s.rpm", e.name, e.version, v.Rel, e.arch))
}

// describeRPM is an rpm's element in each of the three files.
func describeRPM(href string, data []byte) (entry, versionXML, error) {
	p, err := readRPM(data)
	if err != nil {
		return entry{}, versionXML{}, err
	}

	r := &rpmReader{}
	h := p.header
	e := entry{name: r.text(h, tagName), version: r.text(h, tagVersion), arch: r.text(h, tagArch), sha: p.sha}
	v := versionXML{Epoch: fmt.Sprint(r.number(h, tagEpoch)), Ver: e.version, Rel: r.text(h, tagRelease)}

	files := r.files(h)
	e.body = r.primary(p, v, href, files)
	e.other = "\n  " + v.xml() + "\n"

	var lists strings.Builder

	lists.WriteString(e.other)

	for _, f := range files {
		lists.WriteString("  " + f.xml() + "\n")
	}

	e.filelists = lists.String()

	return e, v, r.err
}

func (v versionXML) xml() string {
	return xmlElement("version", "", "epoch", v.Epoch, "ver", v.Ver, "rel", v.Rel)
}

// primary is a package's element in primary.xml, in createrepo's order.
func (r *rpmReader) primary(p rpmPackage, v versionXML, href string, files []rpmFile) string {
	h := p.header
	build := fmt.Sprint(r.number(h, tagBuildTime))

	archive := r.number(h, tagArchiveSize)
	if archive == 0 {
		archive = r.number(p.signature, sigTagPayloadSize)
	}

	var b strings.Builder

	line := func(indent, element string) { b.WriteString(indent + element + "\n") }

	b.WriteString("\n")
	line("  ", xmlElement("name", r.text(h, tagName)))
	line("  ", xmlElement("arch", r.text(h, tagArch)))
	line("  ", v.xml())
	line("  ", xmlElement("checksum", p.sha, "type", "sha256", "pkgid", "YES"))
	line("  ", xmlElement("summary", r.text(h, tagSummary)))
	line("  ", xmlElement("description", r.text(h, tagDescription)))
	line("  ", xmlElement("packager", r.text(h, tagPackager)))
	line("  ", xmlElement("url", r.text(h, tagURL)))
	// The build's time twice: the file's own is when it was copied about.
	line("  ", xmlElement("time", "", "file", build, "build", build))
	line("  ", xmlElement("size", "", "package", fmt.Sprint(p.size),
		"installed", fmt.Sprint(r.number(h, tagSize)), "archive", fmt.Sprint(archive)))
	line("  ", xmlElement("location", "", "href", href))
	line("  ", "<format>")

	for _, f := range []struct {
		name string
		tag  int
	}{
		{"license", tagLicense},
		{"vendor", tagVendor},
		{"group", tagGroup},
		{"buildhost", tagBuildHost},
		{"sourcerpm", tagSourceRPM},
	} {
		line("    ", xmlElement("rpm:"+f.name, r.text(h, f.tag)))
	}

	line("    ", xmlElement("rpm:header-range", "", "start", fmt.Sprint(p.start), "end", fmt.Sprint(p.end)))

	for _, d := range rpmDependencies {
		r.writeDependencies(&b, d.element, r.dependencies(h, d.names, d.flags, d.version))
	}

	for _, f := range files {
		if primaryFile.MatchString(f.path) {
			line("    ", f.xml())
		}
	}

	line("  ", "</format>")

	return b.String()
}

func (r *rpmReader) writeDependencies(b *strings.Builder, element string, entries []rpmEntryXML) {
	if len(entries) == 0 {
		return
	}

	b.WriteString("    <rpm:" + element + ">\n")

	for _, e := range entries {
		b.WriteString("      " + xmlElement("rpm:entry", "",
			"name", e.Name, "flags", e.Flags, "epoch", e.Epoch, "ver", e.Ver, "rel", e.Rel) + "\n")
	}

	b.WriteString("    </rpm:" + element + ">\n")
}

func (f rpmFile) xml() string {
	switch {
	case f.dir:
		return xmlElement("file", f.path, "type", "dir")
	case f.ghost:
		return xmlElement("file", f.path, "type", "ghost")
	default:
		return xmlElement("file", f.path)
	}
}

// yumFiles are the yum repository's files, less the signature.
func (r *reader) yumFiles(entries []entry, date time.Time) (map[string][]byte, error) {
	out := map[string][]byte{}

	var repomd strings.Builder

	fmt.Fprintf(&repomd, "%s<repomd xmlns=\"http://linux.duke.edu/metadata/repo\" xmlns:rpm=\"http://linux.duke.edu/metadata/rpm\">\n"+
		"  <revision>%d</revision>\n", xmlHeader, date.Unix())

	for _, d := range yumData {
		plain := yumDocument(d.name, d.root, entries)

		zipped, err := r.zip(plain)
		if err != nil {
			return nil, err
		}

		href := path.Join("repodata", sha256Hex(zipped)+"-"+d.name+".xml.gz")
		out[yumDir+href] = zipped

		fmt.Fprintf(&repomd, "  <data type=%q>\n    <checksum type=\"sha256\">%s</checksum>\n"+
			"    <open-checksum type=\"sha256\">%s</open-checksum>\n    <location href=%q/>\n"+
			"    <timestamp>%d</timestamp>\n    <size>%d</size>\n    <open-size>%d</open-size>\n  </data>\n",
			d.name, sha256Hex(zipped), sha256Hex(plain), href, date.Unix(), len(zipped), len(plain))
	}

	repomd.WriteString("</repomd>\n")
	out[yumDir+repomdFile] = []byte(repomd.String())

	return out, nil
}

func yumDocument(name, root string, entries []entry) []byte {
	var b bytes.Buffer

	fmt.Fprintf(&b, "%s<%s packages=\"%d\">\n", xmlHeader, root, len(entries))

	for _, e := range entries {
		attrs := xmlAttrs("pkgid", e.sha, "name", e.name, "arch", e.arch)

		switch name {
		case "primary":
			fmt.Fprintf(&b, "<package type=\"rpm\">%s</package>\n", e.body)
		case "filelists":
			fmt.Fprintf(&b, "<package%s>%s</package>\n", attrs, e.filelists)
		default:
			fmt.Fprintf(&b, "<package%s>%s</package>\n", attrs, e.other)
		}
	}

	element, _, _ := strings.Cut(root, " ")
	fmt.Fprintf(&b, "</%s>\n", element)

	return b.Bytes()
}

// xmlElement is one element, empty when text is and it has attributes.
func xmlElement(name, text string, attrs ...string) string {
	if text == "" && len(attrs) > 0 {
		return "<" + name + xmlAttrs(attrs...) + "/>"
	}

	return "<" + name + xmlAttrs(attrs...) + ">" + xmlEscape(text) + "</" + name + ">"
}

// xmlAttrs are attributes given as names and values in turn. One with no value
// is left out.
func xmlAttrs(pairs ...string) string {
	var b strings.Builder

	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			b.WriteString(" " + pairs[i] + `="` + xmlEscape(pairs[i+1]) + `"`)
		}
	}

	return b.String()
}

func xmlEscape(s string) string {
	var b bytes.Buffer

	_ = xml.EscapeText(&b, []byte(s))

	return b.String()
}
