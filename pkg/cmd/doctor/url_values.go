package doctor

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"unicode"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/internal/textescape"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// checkURLValues lists the config values that are URLs and the layer that set
// each, so an endpoint from a file the user did not write is seen before a
// credential is sent to it. Only plaintext http warns. Advisory: the request
// is guarded by the code that sends it, not here. A value the tool ships as
// its own default for that key is the author's choice and is not listed,
// whichever layer repeats it: init writes the defaults into the user's file.
func checkURLValues(_ context.Context, props *p.Props) CheckResult {
	const name = "Config URLs"

	if props.Config == nil {
		return CheckResult{Name: name, Status: CheckSkip, Message: "no configuration loaded"}
	}

	snapshot := props.Config.View().Snapshot()
	shipped := shippedValues(snapshot)

	var (
		lines     []string
		plaintext int
	)

	for _, key := range snapshot.Keys() {
		value, _ := snapshot.Get(key)
		origin, _ := snapshot.Origin(key)

		for _, candidate := range stringValues(value) {
			endpoint, ok := networkEndpoint(candidate)
			if !ok || slices.Contains(shipped[key], candidate) {
				continue
			}

			line := fmt.Sprintf("%s is %s from %s",
				textescape.Unless(unicode.IsPrint, key), endpoint, textescape.Unless(unicode.IsPrint, origin.String()))

			if strings.HasPrefix(endpoint, "http://") {
				plaintext++
				line += " (plaintext)"
			}

			lines = append(lines, line)
		}
	}

	if len(lines) == 0 {
		return CheckResult{Name: name, Status: CheckPass, Message: "no URLs set other than " + props.Tool.Name + "'s own defaults"}
	}

	count := fmt.Sprintf("%d config value(s) are URLs other than %s's own defaults", len(lines), props.Tool.Name)

	result := CheckResult{
		Name:    name,
		Status:  CheckPass,
		Message: count + ", none over plaintext http",
		Details: strings.Join(lines, "\n"),
	}

	if plaintext > 0 {
		result.Status = CheckWarn
		result.Message = fmt.Sprintf("%s, %d over plaintext http; a credential sent over http can be read in transit", count, plaintext)
	}

	return result
}

// shippedValues returns, per key, the strings the tool's compiled-in defaults
// hold there.
func shippedValues(snapshot *config.Snapshot) map[string][]string {
	shipped := map[string][]string{}

	for _, layer := range snapshot.Layers() {
		if layer.Source.Kind == config.SourceDefault {
			collectLeaves(shipped, "", layer.Values)
		}
	}

	return shipped
}

func collectLeaves(into map[string][]string, prefix string, values map[string]any) {
	for key, value := range values {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		if nested, ok := value.(map[string]any); ok {
			collectLeaves(into, path, nested)

			continue
		}

		into[path] = append(into[path], stringValues(value)...)
	}
}

// stringValues returns every string inside a leaf value. A list is a leaf to
// the store, so a URL inside a list of maps is reachable only from here.
func stringValues(value any) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case []any:
		var out []string

		for _, item := range v {
			out = append(out, stringValues(item)...)
		}

		return out
	case map[string]any:
		var out []string

		for _, k := range slices.Sorted(maps.Keys(v)) {
			out = append(out, stringValues(v[k])...)
		}

		return out
	default:
		return nil
	}
}

// hostWithheld stands in for a host that may be part of a credential.
const hostWithheld = "(host withheld)"

// networkEndpoint returns a URL's scheme and host. Userinfo, path, query and
// fragment can each carry a secret, so none is ever returned, and the host is
// replaced by hostWithheld when it may be part of one.
func networkEndpoint(value string) (string, bool) {
	raw := strings.TrimSpace(value)

	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}

	if atSignFollowsAuthority(raw) {
		return u.Scheme + "://" + hostWithheld, true
	}

	return u.Scheme + "://" + textescape.Unless(isPrintableASCII, u.Host), true
}

// atSignFollowsAuthority reports an @ after the first /, ? or # of a URL.
// url.Parse ends the authority there, so userinfo holding one of the three
// unescaped is parsed as the host.
func atSignFollowsAuthority(raw string) bool {
	_, afterScheme, _ := strings.Cut(raw, "://")

	end := strings.IndexAny(afterScheme, "/?#")

	return end >= 0 && strings.Contains(afterScheme[end:], "@")
}

func isPrintableASCII(r rune) bool { return r >= ' ' && r <= '~' }
