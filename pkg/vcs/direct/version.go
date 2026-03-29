package direct

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"strings"

	"github.com/cockroachdb/errors"
	"gopkg.in/yaml.v3"
)

// fetchVersion retrieves the latest version string from a URL.
// Format is auto-detected from Content-Type unless overridden by the
// version_format param. Supported formats: text, json, yaml, xml.
func fetchVersion(ctx context.Context, client *http.Client, versionURL, formatOverride, versionKey string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, versionURL, nil)
	if err != nil {
		return "", errors.WithStack(err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", errors.WithStack(err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", errors.Newf("version endpoint returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", errors.Wrap(err, "reading version response body")
	}

	format := detectFormat(resp.Header.Get("Content-Type"), formatOverride)

	return parseVersion(body, format, versionKey)
}

// detectFormat resolves the effective format string. The override param takes
// precedence; otherwise Content-Type is used with a plain-text fallback.
func detectFormat(contentType, override string) string {
	if override != "" {
		return strings.ToLower(override)
	}

	ct := strings.ToLower(contentType)

	switch {
	case strings.Contains(ct, "application/json") || strings.Contains(ct, "text/json"):
		return "json"
	case strings.Contains(ct, "application/yaml") || strings.Contains(ct, "text/yaml") ||
		strings.Contains(ct, "application/x-yaml"):
		return "yaml"
	case strings.Contains(ct, "application/xml") || strings.Contains(ct, "text/xml"):
		return "xml"
	default:
		return "text"
	}
}

// parseVersion extracts a version string from body according to format.
func parseVersion(body []byte, format, versionKey string) (string, error) {
	switch format {
	case "json":
		return parseJSONVersion(body, versionKey)
	case "yaml":
		return parseYAMLVersion(body, versionKey)
	case "xml":
		return parseXMLVersion(body, versionKey)
	default:
		return strings.TrimSpace(string(body)), nil
	}
}

func parseJSONVersion(body []byte, versionKey string) (string, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return "", errors.Wrap(err, "parsing JSON version response")
	}

	return extractFromMap(func(key string) (string, bool) {
		raw, ok := m[key]
		if !ok {
			return "", false
		}

		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", false
		}

		return s, true
	}, versionKey)
}

func parseYAMLVersion(body []byte, versionKey string) (string, error) {
	var m map[string]any
	if err := yaml.Unmarshal(body, &m); err != nil {
		return "", errors.Wrap(err, "parsing YAML version response")
	}

	return extractFromMap(func(key string) (string, bool) {
		v, ok := m[key]
		if !ok {
			return "", false
		}

		s, ok := v.(string)

		return s, ok
	}, versionKey)
}

// xmlElement is a single child element captured from an XML document.
type xmlElement struct {
	XMLName xml.Name `xml:""`
	Value   string   `xml:",chardata"`
}

// xmlDoc is the top-level XML document used for version extraction.
type xmlDoc struct {
	XMLName  xml.Name     `xml:""`
	Children []xmlElement `xml:",any"`
}

func parseXMLVersion(body []byte, versionKey string) (string, error) {
	keys := resolveKeys(versionKey)

	var doc xmlDoc
	if err := xml.Unmarshal(body, &doc); err != nil {
		return "", errors.Wrap(err, "parsing XML version response")
	}

	for _, key := range keys {
		for _, child := range doc.Children {
			if strings.EqualFold(child.XMLName.Local, key) {
				val := strings.TrimSpace(child.Value)
				if val != "" {
					return val, nil
				}
			}
		}
	}

	return "", errors.WithHintf(
		errors.Newf("version key not found in XML response"),
		"Tried keys: %v. Set version_key in Params to specify the XML element name.",
		keys,
	)
}

// extractFromMap tries versionKey first, then fallback keys tag_name and version.
func extractFromMap(get func(string) (string, bool), versionKey string) (string, error) {
	keys := resolveKeys(versionKey)

	for _, key := range keys {
		if val, ok := get(key); ok && val != "" {
			return val, nil
		}
	}

	return "", errors.WithHintf(
		errors.Newf("version key not found in response"),
		"Tried keys: %v. Set version_key in Params to specify the field name.",
		keys,
	)
}

// resolveKeys builds the ordered list of keys to attempt.
func resolveKeys(versionKey string) []string {
	if versionKey != "" {
		return []string{versionKey}
	}

	return []string{"tag_name", "version"}
}
