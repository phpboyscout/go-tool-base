package openpgpkey

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // SHA-1 is the IETF-mandated WKD bucket-id hash (draft-koch-openpgp-webkey-service §3.1); not a security primitive — see docs/development/specs/2026-06-09-keys-wkd-command.md D4.
	"fmt"
	"io"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/cockroachdb/errors"
)

// File permissions for WKD-tree output. The WKD payload is
// intentionally world-readable — distributing the served files is
// the whole point of publishing a Web Key Directory — so 0o644 / 0o755
// are deliberate (not the security-sensitive defaults that
// gosec G306 / mnd defaults expect).
const (
	wkdFileMode = 0o644
	wkdDirMode  = 0o755
)

// Method selects the WKD URL layout per draft-koch-openpgp-webkey-service §3.1.
type Method string

const (
	// MethodAdvanced is the dedicated-subdomain layout served from
	// openpgpkey.<domain>; URLs include an extra <domain>/ path
	// segment (...openpgpkey/<domain>/hu/<hash>). This is the layout
	// used by the gtb release-signing endpoint at
	// openpgpkey.phpboyscout.uk.
	MethodAdvanced Method = "advanced"

	// MethodDirect is the same-domain layout served from <domain>
	// itself; URLs omit the extra <domain>/ segment
	// (...openpgpkey/hu/<hash>). GPG clients fall back to this when
	// the advanced URL is missing.
	MethodDirect Method = "direct"
)

// Entry binds one email address to the public keys to publish under
// it. The Email's local part is what gets hashed into the hu/<hash>
// filename; the Keys are concatenated (binary OpenPGP packets) into
// that file.
//
// Multiple Entry values with the same Email merge: their Keys are
// pooled into a single hu/ file. Re-using Entry values with the same
// Email is therefore equivalent to passing a single Entry with all
// the keys.
type Entry struct {
	// Email must contain a parseable RFC 5322 address. Only the
	// local-part is used (lowercased, ASCII) for WKD hashing.
	Email string

	// Keys are the public-key bytes to publish under Email. Each
	// element may be either ASCII-armored or already-binary OpenPGP;
	// WriteWKDTree auto-detects and dearmors as needed before writing
	// the hu/ file (which must contain binary packets per RFC §3.1).
	Keys [][]byte
}

// Options configures non-per-entry WKD emission settings.
type Options struct {
	// Method is the URL layout to emit. Zero value defaults to
	// MethodAdvanced (the dedicated-subdomain layout).
	Method Method

	// SubmissionAddress, if non-empty, is written to the domain
	// directory's submission-address file. WKS-aware clients fetch
	// this to discover where to send key-submission requests. Empty
	// means "do not emit submission-address" (still emits policy,
	// which is always required by RFC §3.1).
	SubmissionAddress string
}

// WriteWKDTree writes a complete WKD directory layout under outDir.
//
// For each unique Email across entries, it computes the WKD hu/<hash>
// filename, concatenates that email's binary OpenPGP key packets
// (sorted by primary-key fingerprint for reproducible output), and
// writes the resulting file. Alongside hu/ it writes a zero-byte
// policy file (RFC-required) and, if opts.SubmissionAddress is
// non-empty, a submission-address file.
//
// The full layout for advanced method (the default):
//
//	outDir/.well-known/openpgpkey/<domain>/
//	├── policy                  (zero bytes)
//	├── submission-address      (optional — only when opts.SubmissionAddress != "")
//	└── hu/<z-base-32-hash>     (one per unique email)
//
// Direct method drops the <domain>/ level:
//
//	outDir/.well-known/openpgpkey/
//	├── policy
//	├── submission-address?
//	└── hu/<z-base-32-hash>
//
// Returns the relative paths (under outDir) of every file written, in
// the order written, for callers that want to log or assert against
// the result.
//
// WriteWKDTree refuses to operate outside outDir/.well-known/openpgpkey
// (path-traversal defence against pathological domain values).
//
// Errors:
//   - if domain is empty, contains a path separator, or is otherwise
//     invalid as a hostname;
//   - if an Entry's Email cannot be parsed by net/mail;
//   - if a key cannot be parsed as armored or binary OpenPGP;
//   - if no entries are supplied (an empty WKD tree is never useful).
func WriteWKDTree(outDir, domain string, opts Options, entries ...Entry) ([]string, error) {
	method, merged, err := validateInputs(domain, opts, entries)
	if err != nil {
		return nil, err
	}

	root := wkdRoot(outDir, domain, method)
	if err := os.MkdirAll(filepath.Join(root, "hu"), wkdDirMode); err != nil {
		return nil, errors.Wrap(err, "creating WKD hu directory")
	}

	written, err := writeDomainMetadata(outDir, root, opts)
	if err != nil {
		return nil, err
	}

	huPaths, err := writeHuFiles(outDir, root, merged)
	if err != nil {
		return nil, err
	}

	return append(written, huPaths...), nil
}

// validateInputs runs the cheap up-front checks that don't touch the
// filesystem: domain shape, method selection, and entry merging.
// Returns the resolved Method and the merged email→keys map.
func validateInputs(domain string, opts Options, entries []Entry) (Method, map[string][][]byte, error) {
	if len(entries) == 0 {
		return "", nil, errors.New("WriteWKDTree: at least one Entry required")
	}

	if err := validateDomain(domain); err != nil {
		return "", nil, err
	}

	method := opts.Method
	if method == "" {
		method = MethodAdvanced
	}

	if method != MethodAdvanced && method != MethodDirect {
		return "", nil, errors.Newf("WriteWKDTree: invalid Method %q (want %q or %q)", method, MethodAdvanced, MethodDirect)
	}

	merged, err := mergeEntriesByEmail(entries)
	if err != nil {
		return "", nil, err
	}

	return method, merged, nil
}

// writeDomainMetadata writes the per-domain metadata files (policy
// always, submission-address when configured). Returns the relative
// paths written.
func writeDomainMetadata(outDir, root string, opts Options) ([]string, error) {
	var written []string

	policyPath := filepath.Join(root, "policy")
	if err := os.WriteFile(policyPath, nil, wkdFileMode); err != nil { //nolint:gosec // WKD payload is intentionally world-readable.
		return nil, errors.Wrap(err, "writing policy")
	}

	written = append(written, relTo(outDir, policyPath))

	if opts.SubmissionAddress == "" {
		return written, nil
	}

	if _, err := mail.ParseAddress(opts.SubmissionAddress); err != nil {
		return nil, errors.Wrapf(err, "WriteWKDTree: SubmissionAddress %q is not a valid email", opts.SubmissionAddress)
	}

	subPath := filepath.Join(root, "submission-address")
	if err := os.WriteFile(subPath, []byte(opts.SubmissionAddress), wkdFileMode); err != nil { //nolint:gosec // WKD payload is intentionally world-readable.
		return nil, errors.Wrap(err, "writing submission-address")
	}

	return append(written, relTo(outDir, subPath)), nil
}

// writeHuFiles emits one hu/<z-base-32-hash> file per email, in
// lexicographic hash order for stable emission.
func writeHuFiles(outDir, root string, merged map[string][][]byte) ([]string, error) {
	emails := make([]string, 0, len(merged))
	for email := range merged {
		emails = append(emails, email)
	}

	sort.Strings(emails)

	var written []string

	for _, email := range emails {
		hash, err := wkdHash(email)
		if err != nil {
			return nil, err
		}

		body, err := concatBinaryKeys(merged[email])
		if err != nil {
			return nil, errors.Wrapf(err, "encoding keys for %q", email)
		}

		huPath := filepath.Join(root, "hu", hash)
		if err := os.WriteFile(huPath, body, wkdFileMode); err != nil { //nolint:gosec // WKD payload is intentionally world-readable.
			return nil, errors.Wrapf(err, "writing hu/%s", hash)
		}

		written = append(written, relTo(outDir, huPath))
	}

	return written, nil
}

// WKDHash returns the z-base-32 encoded SHA-1 of the lowercased
// local-part of an email — i.e. the filename used under hu/ in the
// WKD layout. Exposed so callers can pre-compute the expected URL or
// hash a UID without writing files.
func WKDHash(email string) (string, error) { return wkdHash(email) }

// wkdHash computes the z-base-32 SHA-1 of the lowercased local part.
func wkdHash(email string) (string, error) {
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return "", errors.Wrapf(err, "parsing email %q", email)
	}

	at := strings.LastIndexByte(addr.Address, '@')
	if at <= 0 {
		return "", errors.Newf("email %q has no local part", email)
	}

	local := strings.ToLower(addr.Address[:at])

	sum := sha1.Sum([]byte(local)) //nolint:gosec // RFC-mandated bucket identifier; see file-header comment.

	return zbase32Encode(sum[:]), nil
}

// mergeEntriesByEmail collapses multiple Entry values sharing an
// email into a single keyset.
func mergeEntriesByEmail(entries []Entry) (map[string][][]byte, error) {
	out := map[string][][]byte{}

	for i, e := range entries {
		if e.Email == "" {
			return nil, errors.Newf("WriteWKDTree: Entry[%d].Email is empty", i)
		}

		if _, err := mail.ParseAddress(e.Email); err != nil {
			return nil, errors.Wrapf(err, "WriteWKDTree: Entry[%d].Email %q is not a valid email", i, e.Email)
		}

		if len(e.Keys) == 0 {
			return nil, errors.Newf("WriteWKDTree: Entry[%d] (%s) has no Keys", i, e.Email)
		}

		out[e.Email] = append(out[e.Email], e.Keys...)
	}

	return out, nil
}

// concatBinaryKeys dearmors each key as needed and concatenates the
// resulting binary OpenPGP packet streams. Output is sorted by primary-
// key fingerprint (lexicographic uppercase hex) so the same key set
// produces a byte-identical hu/ file regardless of input order.
func concatBinaryKeys(keys [][]byte) ([]byte, error) {
	type parsed struct {
		fingerprint string
		binary      []byte
	}

	parsedKeys := make([]parsed, 0, len(keys))

	for i, raw := range keys {
		binBytes, ent, err := dearmorIfNeeded(raw)
		if err != nil {
			return nil, errors.Wrapf(err, "key %d", i)
		}

		parsedKeys = append(parsedKeys, parsed{
			fingerprint: strings.ToUpper(fmt.Sprintf("%X", ent.PrimaryKey.Fingerprint)),
			binary:      binBytes,
		})
	}

	sort.Slice(parsedKeys, func(i, j int) bool {
		return parsedKeys[i].fingerprint < parsedKeys[j].fingerprint
	})

	var out bytes.Buffer
	for _, p := range parsedKeys {
		out.Write(p.binary)
	}

	return out.Bytes(), nil
}

// dearmorIfNeeded accepts either ASCII-armored or binary OpenPGP and
// returns the binary form plus the parsed primary entity (used by the
// caller for fingerprint-based sorting).
func dearmorIfNeeded(raw []byte) ([]byte, *openpgp.Entity, error) {
	armorHeader := []byte("-----BEGIN PGP")

	if bytes.HasPrefix(bytes.TrimLeft(raw, " \t\r\n"), armorHeader) {
		block, err := armor.Decode(bytes.NewReader(raw))
		if err != nil {
			return nil, nil, errors.Wrap(err, "decoding armor")
		}

		bin, err := io.ReadAll(block.Body)
		if err != nil {
			return nil, nil, errors.Wrap(err, "reading dearmored body")
		}

		ent, err := openpgp.ReadEntity(packet.NewReader(bytes.NewReader(bin)))
		if err != nil {
			return nil, nil, errors.Wrap(err, "parsing dearmored entity")
		}

		return bin, ent, nil
	}

	// Already binary.
	ent, err := openpgp.ReadEntity(packet.NewReader(bytes.NewReader(raw)))
	if err != nil {
		return nil, nil, errors.Wrap(err, "parsing binary entity")
	}

	return raw, ent, nil
}

// wkdRoot is the directory the WKD files live under inside outDir.
func wkdRoot(outDir, domain string, method Method) string {
	switch method {
	case MethodAdvanced:
		return filepath.Join(outDir, ".well-known", "openpgpkey", domain)
	case MethodDirect:
		return filepath.Join(outDir, ".well-known", "openpgpkey")
	default:
		// validated upstream; unreachable.
		return filepath.Join(outDir, ".well-known", "openpgpkey")
	}
}

// validateDomain refuses values that would let a caller escape the
// outDir tree or otherwise emit a non-hostname path.
func validateDomain(domain string) error {
	switch {
	case domain == "":
		return errors.New("WriteWKDTree: domain must be non-empty")
	case strings.ContainsAny(domain, "/\\"):
		return errors.Newf("WriteWKDTree: domain %q contains a path separator", domain)
	case strings.Contains(domain, ".."):
		return errors.Newf("WriteWKDTree: domain %q contains '..'", domain)
	case strings.HasPrefix(domain, "."), strings.HasSuffix(domain, "."):
		return errors.Newf("WriteWKDTree: domain %q has a leading or trailing dot", domain)
	}

	for _, r := range domain {
		if !isHostnameRune(r) {
			return errors.Newf("WriteWKDTree: domain %q contains invalid character %q", domain, r)
		}
	}

	return nil
}

// isHostnameRune reports whether r is permitted in a DNS hostname
// label per the WKD-relevant subset of RFC 1035 (letters, digits,
// hyphens, and the label separator '.').
func isHostnameRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '.' || r == '-':
		return true
	default:
		return false
	}
}

func relTo(base, full string) string {
	rel, err := filepath.Rel(base, full)
	if err != nil {
		return full
	}

	return rel
}
