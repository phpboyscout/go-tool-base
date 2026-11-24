package main

import (
	"archive/tar"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// node is an XML element, without the namespace a fragment does not declare.
type node struct {
	XMLName  xml.Name   `xml:""`
	Attrs    []xml.Attr `xml:",any,attr"`
	Text     string     `xml:",chardata"`
	Children []node     `xml:",any"`
}

// flatten lists a node's elements, one line each, in order. createrepo's
// description keeps the newline the package ends it with, which is in the
// package, so text is compared whole.
func (n node) flatten(prefix string) []string {
	path := prefix + "/" + n.XMLName.Local

	var attrs []string
	for _, a := range n.Attrs {
		attrs = append(attrs, a.Name.Local+"="+a.Value)
	}

	text := n.Text
	if len(n.Children) > 0 {
		text = ""
	}

	out := []string{fmt.Sprintf("%s %v %q", path, attrs, text)}
	for _, c := range n.Children {
		out = append(out, c.flatten(path)...)
	}

	return out
}

func packageNamed(t *testing.T, doc []byte, sha string) []string {
	t.Helper()

	var root node
	require.NoError(t, xml.Unmarshal(doc, &root))

	for _, p := range root.Children {
		if lines := p.flatten(""); strings.Contains(strings.Join(lines, "\n"), sha) {
			return lines
		}
	}

	t.Fatalf("no package %s", sha)

	return nil
}

func TestAnRPMIsListedAsCreaterepoListsIt(t *testing.T) {
	t.Parallel()

	for _, file := range []string{
		filepath.Join("createrepo", "rich-2.0.0-4.x86_64.rpm"),
		filepath.Join("packages", "gtb-1.0.0-1.x86_64.rpm"),
	} {
		t.Run(filepath.Base(file), func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(filepath.Join("testdata", file))
			require.NoError(t, err)

			e, _, err := describeRPM(filepath.Base(file), data)
			require.NoError(t, err)

			for _, d := range yumData {
				want, err := os.ReadFile(filepath.Join("testdata", "createrepo", d.name+".xml"))
				require.NoError(t, err)

				theirs := packageNamed(t, want, e.sha)
				ours := packageNamed(t, yumDocument(d.name, d.root, []entry{e}), e.sha)

				for i := range theirs {
					// When the file was copied, which is the build's time here.
					if strings.HasPrefix(theirs[i], "/package/time ") {
						theirs[i] = "/package/time [file=1767225600 build=1767225600] \"\""
					}
				}

				assert.Equal(t, theirs, ours, d.name)
			}
		})
	}
}

// deb is a deb holding one control file.
func deb(t *testing.T, member, name, control string) []byte {
	t.Helper()

	var tarball bytes.Buffer

	w := tar.NewWriter(&tarball)
	require.NoError(t, w.WriteHeader(&tar.Header{Name: name, Size: int64(len(control)), Mode: 0o644}))
	_, err := w.Write([]byte(control))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	zipped, err := gzipBytes(tarball.Bytes())
	require.NoError(t, err)

	return ar(member, zipped)
}

func ar(member string, data []byte) []byte {
	out := []byte(arMagic)
	out = fmt.Appendf(out, "%-16s%-12s%-6s%-6s%-8s%-10d`\n", "debian-binary", "0", "0", "0", "100644", 3)
	out = append(out, "2.\n\n"...)
	out = fmt.Appendf(out, "%-16s%-12s%-6s%-6s%-8s%-10d`\n", member, "0", "0", "0", "100644", len(data))

	return append(out, data...)
}

const control = "Package: gtb\nVersion: 1.0.0\nArchitecture: amd64\n"

func TestADebsControlFileIsRead(t *testing.T) {
	t.Parallel()

	for name, d := range map[string][]byte{
		"control":          deb(t, "control.tar.gz", "control", control),
		"./control":        deb(t, "control.tar.gz", "./control", control),
		"a trailing slash": deb(t, "control.tar.gz/", "control", control),
		"crlf":             deb(t, "control.tar.gz", "control", strings.ReplaceAll(control, "\n", "\r\n")),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, e, err := readDeb(d)
			require.NoError(t, err)
			assert.Equal(t, strings.TrimSpace(control), got)
			assert.Equal(t, entryKey{"gtb", "1.0.0", "amd64"}, e.key())
			assert.Equal(t, sha256Hex(d), e.sha)
		})
	}
}

func TestADebThatCannotBeListedIsRefused(t *testing.T) {
	t.Parallel()

	whole := deb(t, "control.tar.gz", "control", control)

	badSize := bytes.Clone(whole)
	copy(badSize[len(arMagic)+arSizeStart:], "x         ")

	negative := bytes.Clone(whole)
	copy(negative[len(arMagic)+arSizeStart:], "-9        ")

	for name, d := range map[string][]byte{
		"empty":                     nil,
		"truncated":                 whole[:len(whole)-1],
		"a member's size":           badSize,
		"a member's negative size":  negative,
		"another compression":       ar("control.tar.xz", nil),
		"not gzip":                  ar("control.tar.gz", []byte("plain")),
		"not a tar":                 ar("control.tar.gz", mustGzip(t, []byte(strings.Repeat("x", 600)))),
		"no control file":           deb(t, "control.tar.gz", "md5sums", control),
		"a blank line in control":   deb(t, "control.tar.gz", "control", "Package: gtb\n\nPackage: other\n"),
		"a field set twice":         deb(t, "control.tar.gz", "control", control+"Package: other\n"),
		"a field in both cases":     deb(t, "control.tar.gz", "control", control+"package: other\n"),
		"a continuation first":      deb(t, "control.tar.gz", "control", " more\n"+control),
		"no version":                deb(t, "control.tar.gz", "control", "Package: gtb\nArchitecture: amd64\n"),
		"a version that is not one": deb(t, "control.tar.gz", "control", "Package: gtb\nVersion: newest\nArchitecture: amd64\n"),
		"no package":                deb(t, "control.tar.gz", "control", "Version: 1.0.0\nArchitecture: amd64\n"),
		"a digest of its own":       deb(t, "control.tar.gz", "control", control+"SHA256: 00\n"),
		"a file name in lower case": deb(t, "control.tar.gz", "control", control+"filename: elsewhere.deb\n"),
		"a file name of its own":    deb(t, "control.tar.gz", "control", control+"Filename: elsewhere.deb\n"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, _, err := readDeb(d)
			require.Error(t, err)
		})
	}
}

func TestADebForAnotherArchitectureIsRefused(t *testing.T) {
	t.Parallel()

	d := deb(t, "control.tar.gz", "control", "Package: gtb\nVersion: 1.0.0\nArchitecture: riscv64\n")

	_, err := debEntry("v1.0.0", "gtb_1.0.0_riscv64.deb", d)
	require.ErrorIs(t, err, errPackage)
}

func mustGzip(t *testing.T, data []byte) []byte {
	t.Helper()

	out, err := gzipBytes(data)
	require.NoError(t, err)

	return out
}

// rpmValue is one tag of a header made for a test.
type rpmValue struct {
	tag, kind, count uint32
	data             []byte
}

func size[T any](values []T) uint32 {
	var n uint32
	for range values {
		n++
	}

	return n
}

func text(tag uint32, values ...string) rpmValue {
	return rpmValue{tag, rpmTypeArray, size(values), []byte(strings.Join(values, "\x00") + "\x00")}
}

func numbers(tag uint32, values ...uint32) rpmValue {
	var b []byte
	for _, v := range values {
		b = binary.BigEndian.AppendUint32(b, v)
	}

	return rpmValue{tag, rpmTypeInt32, size(values), b}
}

func rpmHeaderOf(values ...rpmValue) []byte {
	var index, data []byte

	for _, v := range values {
		for _, n := range []uint32{v.tag, v.kind, size(data), v.count} {
			index = binary.BigEndian.AppendUint32(index, n)
		}

		data = append(data, v.data...)
	}

	out := append([]byte(rpmHeaderMagic), 0, 0, 0, 0)
	out = binary.BigEndian.AppendUint32(out, size(values))
	out = binary.BigEndian.AppendUint32(out, size(data))

	return append(append(out, index...), data...)
}

func rpm(values ...rpmValue) []byte {
	lead := append([]byte(rpmLeadMagic), make([]byte, rpmLeadSize-len(rpmLeadMagic))...)

	return append(append(lead, rpmHeaderOf()...), rpmHeaderOf(values...)...)
}

// rpmIdentity is the least an rpm needs to be listed.
func rpmIdentity(values ...rpmValue) []rpmValue {
	return append([]rpmValue{
		text(tagName, "gtb"), text(tagVersion, "1.0.0"), text(tagRelease, "1"), text(tagArch, "x86_64"),
	}, values...)
}

func TestAnRPMWithNothingButItsNameIsListed(t *testing.T) {
	t.Parallel()

	e, err := rpmEntry("v1.0.0", "gtb-1.0.0-1.x86_64.rpm", rpm(rpmIdentity()...))
	require.NoError(t, err)
	assert.Equal(t, entryKey{"gtb", "1.0.0", "x86_64"}, e.key())
	assert.Contains(t, e.body, `<location href="../v1.0.0/gtb-1.0.0-1.x86_64.rpm"/>`)
}

func TestAnRPMThatCannotBeListedIsRefused(t *testing.T) {
	t.Parallel()

	whole := rpm(rpmIdentity()...)
	header := rpmLeadSize + rpmHeaderPrefix

	at := func(offset int, value uint32) []byte {
		out := bytes.Clone(whole)
		binary.BigEndian.PutUint32(out[offset:], value)

		return out
	}

	for name, d := range map[string][]byte{
		"empty":                     nil,
		"no signature header":       whole[:rpmLeadSize+8],
		"no header":                 whole[:header+4],
		"truncated":                 whole[:len(whole)-1],
		"a header with no magic":    at(header, 0),
		"more tags than bytes":      at(header+8, 1<<31),
		"more tags than there are":  at(header+8, 6),
		"a store past the end":      at(header+12, 1<<30),
		"a tag past the store":      at(header+rpmHeaderPrefix+8, 1<<20),
		"more strings than bytes":   at(header+rpmHeaderPrefix+12, 1<<30),
		"more strings than it has":  at(header+rpmHeaderPrefix+12, 8),
		"a name that is numbers":    rpm(numbers(tagName, 1), text(tagVersion, "1.0.0"), text(tagRelease, "1"), text(tagArch, "x86_64")),
		"a size that is text":       rpm(rpmIdentity(text(tagSize, "big"))...),
		"more numbers than bytes":   rpm(rpmIdentity(rpmValue{tagSize, rpmTypeInt32, 4, []byte{0, 0, 0, 1}})...),
		"no architecture":           rpm(text(tagName, "gtb"), text(tagVersion, "1.0.0"), text(tagRelease, "1")),
		"another release":           rpm(text(tagName, "gtb"), text(tagVersion, "1.0.0"), text(tagRelease, "2"), text(tagArch, "x86_64")),
		"an epoch":                  rpm(rpmIdentity(numbers(tagEpoch, 1))...),
		"dependencies of two sizes": rpm(rpmIdentity(text(1049, "git"))...),
		"files of two sizes":        rpm(rpmIdentity(text(tagBaseNames, "gtb"))...),
		"a file in no directory": rpm(rpmIdentity(text(tagBaseNames, "gtb"), text(tagDirNames, "/usr/bin/"),
			numbers(tagDirIndexes, 1), numbers(tagFileModes, 0), numbers(tagFileFlags, 0))...),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := rpmEntry("v1.0.0", "gtb-1.0.0-1.x86_64.rpm", d)
			require.ErrorIs(t, err, errPackage)
		})
	}
}

func TestAnRPMOfAnotherReleaseIsRefusedUnderItsOwnName(t *testing.T) {
	t.Parallel()

	d := rpm(text(tagName, "gtb"), text(tagVersion, "1.0.0"), text(tagRelease, "2"), text(tagArch, "x86_64"))

	_, err := rpmEntry("v1.0.0", "gtb-1.0.0-2.x86_64.rpm", d)
	require.ErrorIs(t, err, errPackage)
}

func TestAnRPMForAnotherArchitectureIsRefused(t *testing.T) {
	t.Parallel()

	d := rpm(text(tagName, "gtb"), text(tagVersion, "1.0.0"), text(tagRelease, "1"), text(tagArch, "riscv64"))

	_, err := rpmEntry("v1.0.0", "gtb-1.0.0-1.riscv64.rpm", d)
	require.ErrorIs(t, err, errPackage)
}

func TestARequirementOnTheRPMFormatIsLeftOut(t *testing.T) {
	t.Parallel()

	e, err := rpmEntry("v1.0.0", "gtb-1.0.0-1.x86_64.rpm", rpm(rpmIdentity(
		text(1049, "rpmlib(CompressedFileNames)", "git"), numbers(1048, rpmSenseLess|rpmSenseEqual, 0), text(1050, "3.0.4-1", ""),
	)...))
	require.NoError(t, err)
	assert.NotContains(t, e.body, "rpmlib")
	assert.Contains(t, e.body, `<rpm:entry name="git"/>`)
}

func TestAVersionIsSplitAsRPMSplitsIt(t *testing.T) {
	t.Parallel()

	for in, want := range map[string][3]string{
		"1.0":       {"0", "1.0", ""},
		"1.0-2":     {"0", "1.0", "2"},
		"3:1.0-2":   {"3", "1.0", "2"},
		"1.0-rc1-2": {"0", "1.0-rc1", "2"},
		"a:1.0":     {"0", "a:1.0", ""},
	} {
		epoch, ver, rel := splitEVR(in)
		assert.Equal(t, want, [3]string{epoch, ver, rel}, in)
	}
}
