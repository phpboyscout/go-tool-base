package keys

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/openpgpkey"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// NewCmdKeysWKD returns the `gtb keys wkd` subcommand. Reads one or
// more armored OpenPGP public keys and emits a Web Key Directory tree
// (per draft-koch-openpgp-webkey-service §3.1) ready to upload to a
// static host.
//
// Spec: docs/development/specs/2026-06-09-keys-wkd-command.md.
func NewCmdKeysWKD(p *props.Props) *setup.Command {
	var (
		domain            string
		emails            []string
		output            string
		method            string
		submissionAddress string
	)

	cmd := &cobra.Command{
		Use:   "wkd <public-key.asc> [<public-key.asc>...]",
		Short: "Generate a Web Key Directory tree from one or more public keys",
		Long: `Read one or more ASCII-armored OpenPGP public keys, group them by
email (configured via --email), and write the WKD layout expected by
draft-koch-openpgp-webkey-service §3.1 under --output. The resulting
tree is ready to upload to a static host (e.g. Cloudflare Pages).

Each --email may be passed multiple times. For each email, keys whose
UIDs contain that email are grouped into a single hu/<z-base-32-hash>
file, concatenated in lexicographic fingerprint order for reproducible
output. A key with multiple matching UIDs ends up under every matching
email's bucket (standard WKD behaviour).

Examples:

  # Two keys, one email — typical phpboyscout setup
  gtb keys wkd \
      --domain phpboyscout.uk \
      --email release@phpboyscout.uk \
      --output ./wkd-staging \
      rotation-authority.asc signing-key-v1.asc

  # Two emails, separate hu/ buckets
  gtb keys wkd \
      --domain phpboyscout.uk \
      --email release@phpboyscout.uk \
      --email security@phpboyscout.uk \
      --output ./wkd-staging \
      keys/*.asc

  # Direct method (same-domain layout, no openpgpkey.<domain> subdomain)
  gtb keys wkd --method direct ...`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runWKD(p, args, domain, emails, output, method, submissionAddress)
		},
	}

	cmd.Flags().StringVar(&domain, "domain", "",
		"DNS domain serving the WKD endpoint (required; e.g. phpboyscout.uk). "+
			"For the advanced method this domain appears in the URL path; for direct it doesn't.")
	cmd.Flags().StringSliceVar(&emails, "email", nil,
		"Email address(es) to publish under. Repeatable. Keys whose UIDs match are grouped into that email's hu/<hash> bucket. "+
			"If omitted, every distinct email found across input keys is published.")
	cmd.Flags().StringVar(&output, "output", "./wkd-staging",
		"Output directory; receives a .well-known/openpgpkey/... tree.")
	cmd.Flags().StringVar(&method, "method", string(openpgpkey.MethodAdvanced),
		`URL layout: "advanced" (default; served from openpgpkey.<domain>) or "direct" (served from <domain> itself).`)
	cmd.Flags().StringVar(&submissionAddress, "submission-address", "",
		"Email address to write to the WKD submission-address file. Empty (default) means do not emit the file. "+
			`Pass "auto" to use the first --email; pass an explicit address to override.`)

	_ = cmd.MarkFlagRequired("domain")

	return setup.Wrap("", cmd)
}

// parsedKey binds one entity's own armored bytes to the emails its UIDs
// contain. We re-serialize each entity individually (rather than carry
// the whole input file's bytes) so that a ring file holding several
// entities does not duplicate every entity's packets into every bucket —
// each parsedKey publishes exactly the one entity it represents.
type parsedKey struct {
	raw    []byte
	emails []string
}

func runWKD(p *props.Props, keyPaths []string, domain string, emails []string, output, method, submissionAddress string) error {
	parsed, err := parseKeyFiles(keyPaths)
	if err != nil {
		return err
	}

	emails = resolveEmails(emails, parsed)

	buckets, err := groupByEmail(emails, parsed)
	if err != nil {
		return err
	}

	// Empty means omit the submission-address file entirely (matches the
	// --submission-address help and openpgpkey.WriteWKDTree's contract).
	// The explicit "auto" sentinel opts in to the first --email; any
	// other value is used verbatim.
	if submissionAddress == "auto" {
		if len(emails) == 0 {
			return errors.New(`--submission-address auto requires at least one resolvable --email`)
		}

		submissionAddress = emails[0]
	}

	entries := make([]openpgpkey.Entry, 0, len(buckets))
	for _, b := range buckets {
		entries = append(entries, openpgpkey.Entry{Email: b.email, Keys: b.keys})
	}

	written, err := openpgpkey.WriteWKDTree(output, domain, openpgpkey.Options{
		Method:            openpgpkey.Method(method),
		SubmissionAddress: submissionAddress,
	}, entries...)
	if err != nil {
		return errors.Wrap(err, "writing WKD tree")
	}

	logWKDOutcome(p, buckets, written, output, method)

	return nil
}

// parseKeyFiles reads every input file and groups them by the emails
// each contained entity claims via its UIDs. One file may produce
// multiple parsedKey entries when it holds multiple entities (a
// key ring), or when an entity has multiple UIDs.
func parseKeyFiles(keyPaths []string) ([]parsedKey, error) {
	parsed := make([]parsedKey, 0, len(keyPaths))

	for _, kp := range keyPaths {
		raw, err := os.ReadFile(kp)
		if err != nil {
			return nil, errors.Wrapf(err, "reading %s", kp)
		}

		ring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(string(raw)))
		if err != nil {
			return nil, errors.Wrapf(err, "parsing %s as armored OpenPGP", kp)
		}

		for _, ent := range ring {
			extracted := extractEmails(ent)
			if len(extracted) == 0 {
				return nil, errors.Newf("key %s has no UID with a parseable email", kp)
			}

			entityBytes, serErr := serializeEntity(ent)
			if serErr != nil {
				return nil, errors.Wrapf(serErr, "serializing a key from %s", kp)
			}

			parsed = append(parsed, parsedKey{raw: entityBytes, emails: extracted})
		}
	}

	return parsed, nil
}

// serializeEntity re-encodes a single parsed entity to its own
// ASCII-armored public-key block. This is what lets parseKeyFiles attach
// per-entity bytes instead of the whole-file bytes, so a multi-entity
// ring file doesn't cross-publish every entity into every bucket.
func serializeEntity(ent *openpgp.Entity) ([]byte, error) {
	var buf bytes.Buffer

	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		return nil, errors.Wrap(err, "opening armor writer")
	}

	if err := ent.Serialize(w); err != nil {
		return nil, errors.Wrap(err, "serializing entity")
	}

	if err := w.Close(); err != nil {
		return nil, errors.Wrap(err, "closing armor writer")
	}

	return buf.Bytes(), nil
}

// extractEmails returns the distinct, non-empty emails from every UID
// of an entity.
func extractEmails(ent *openpgp.Entity) []string {
	seen := map[string]bool{}

	var out []string

	for _, uid := range ent.Identities {
		if uid.UserId == nil {
			continue
		}

		email := uid.UserId.Email
		if email == "" || seen[email] {
			continue
		}

		out = append(out, email)
		seen[email] = true
	}

	return out
}

// resolveEmails returns the supplied list verbatim if non-empty;
// otherwise discovers the set of distinct emails across the parsed
// keys and returns those.
func resolveEmails(emails []string, parsed []parsedKey) []string {
	if len(emails) > 0 {
		return emails
	}

	seen := map[string]struct{}{}

	var out []string

	for _, pk := range parsed {
		for _, e := range pk.emails {
			if _, dup := seen[e]; dup {
				continue
			}

			seen[e] = struct{}{}

			out = append(out, e)
		}
	}

	return out
}

// bucket binds an email to the raw key bytes destined for its hu/ file.
type bucket struct {
	email string
	keys  [][]byte
}

// groupByEmail assigns each parsedKey to the bucket(s) of every
// requested email it matches. Errors if a requested email has no
// matching key.
func groupByEmail(emails []string, parsed []parsedKey) ([]bucket, error) {
	buckets := make([]bucket, 0, len(emails))

	for _, e := range emails {
		var keys [][]byte

		for _, pk := range parsed {
			if containsEmail(pk.emails, e) {
				keys = append(keys, pk.raw)
			}
		}

		if len(keys) == 0 {
			return nil, errors.Newf("no input key matched --email %s", e)
		}

		buckets = append(buckets, bucket{email: e, keys: keys})
	}

	return buckets, nil
}

func containsEmail(emails []string, want string) bool {
	for _, e := range emails {
		if e == want {
			return true
		}
	}

	return false
}

func logWKDOutcome(p props.LoggerProvider, buckets []bucket, written []string, output, method string) {
	for _, b := range buckets {
		hash, _ := openpgpkey.WKDHash(b.email)
		p.GetLogger().Info("WKD bucket",
			"email", b.email,
			"hash", hash,
			"keys", len(b.keys),
		)
	}

	for _, w := range written {
		p.GetLogger().Info("wrote", "path", filepath.Join(output, w))
	}

	p.GetLogger().Info("WKD tree complete",
		"output", output,
		"method", method,
		"emails", len(buckets),
		"files", len(written),
	)
}
