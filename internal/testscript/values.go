package testscript

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Test inputs live outside the script so a test is portable. The script says
// which value it needs; the machine says what that value is.
//
// Three layers, lowest priority first:
//
//	shop.test.properties            committed — the defaults everyone shares
//	shop.test.override.properties   gitignored — this machine's differences
//	ATR_VALUE_SEARCH_TERM           environment — CI, where files are awkward
//
// Credentials do not belong in any of them. A gitignored file is not an
// encrypted one, and it gets copied around. Put a secret's *name* in a value
// and fetch it with atr.fillSecret, which never lets the value reach the
// script at all.

// Properties file suffixes, appended after the spec's stem.
const (
	valuesSuffix   = ".properties"
	overrideSuffix = ".override.properties"
	// envPrefix maps ATR_VALUE_SEARCH_TERM onto the key "search_term".
	envPrefix = "ATR_VALUE_"
)

// Values is a resolved set of test inputs.
type Values struct {
	// values is the merged result.
	values map[string]string
	// origin records which layer each key came from, so an error can say
	// where a value did or did not come from.
	origin map[string]string
	// sources lists the files consulted, present or not.
	sources []string
	// exp runs and caches command substitutions.
	exp expander
}

// ValuesPath returns the committed properties path for a spec.
func ValuesPath(specPath string) string {
	return stem(specPath) + valuesSuffix
}

// OverridePath returns the per-machine properties path for a spec.
func OverridePath(specPath string) string {
	return stem(specPath) + overrideSuffix
}

// stem strips the final extension: shop.test.txt -> shop.test.
func stem(specPath string) string {
	return strings.TrimSuffix(specPath, filepath.Ext(specPath))
}

// LoadValues resolves the three layers for a spec. Missing files are not an
// error: a test with no inputs needs no properties file, and the override is
// per-machine by definition.
func LoadValues(specPath string) (*Values, error) {
	v := &Values{
		values: make(map[string]string),
		origin: make(map[string]string),
	}

	for _, layer := range []struct{ path, name string }{
		{ValuesPath(specPath), "values"},
		{OverridePath(specPath), "override"},
	} {
		v.sources = append(v.sources, layer.path)

		data, err := os.ReadFile(layer.path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading %s: %w", layer.path, err)
		}

		parsed, err := ParseProperties(string(data))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", layer.path, err)
		}
		for key, val := range parsed {
			v.values[key] = val
			v.origin[key] = layer.name
		}
	}

	// Environment wins: it is the layer a CI job can set without writing a
	// file into a checkout.
	for _, entry := range os.Environ() {
		name, val, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(name, envPrefix) {
			continue
		}
		key := strings.ToLower(strings.TrimPrefix(name, envPrefix))
		if key == "" {
			continue
		}
		v.values[key] = val
		v.origin[key] = "environment"
	}

	return v, nil
}

// NewValues builds a set directly, for tests and callers that already have
// the data.
func NewValues(values map[string]string) *Values {
	v := &Values{
		values: make(map[string]string, len(values)),
		origin: make(map[string]string, len(values)),
	}
	for k, val := range values {
		v.values[k] = val
		v.origin[k] = "values"
	}
	return v
}

// Get returns the raw, unexpanded value and whether it was set.
//
// Use Resolve for anything a test consumes. Get exists for listing and
// diagnostics, where running a command to display a key would be wrong.
func (v *Values) Get(key string) (string, bool) {
	if v == nil {
		return "", false
	}
	val, ok := v.values[key]
	return val, ok
}

// Resolve returns a value with $(command) and ${VAR} expanded.
//
// Expansion happens here rather than at load so a command only runs if a test
// actually reads that value — a suite of twenty tests must not trigger twenty
// password prompts to run one of them.
func (v *Values) Resolve(ctx context.Context, key string) (string, bool, error) {
	raw, ok := v.Get(key)
	if !ok {
		return "", false, nil
	}
	out, err := v.exp.expand(ctx, key, raw)
	if err != nil {
		return "", true, err
	}
	return out, true, nil
}

// Keys lists the resolved keys in order.
func (v *Values) Keys() []string {
	if v == nil {
		return nil
	}
	keys := make([]string, 0, len(v.values))
	for k := range v.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Origin reports which layer supplied a key.
func (v *Values) Origin(key string) string {
	if v == nil {
		return ""
	}
	return v.origin[key]
}

// Sources lists the files that were consulted.
func (v *Values) Sources() []string {
	if v == nil {
		return nil
	}
	return v.sources
}

// missingMessage explains a missing key in terms of what the user can do
// about it, naming every place the value could have come from.
func (v *Values) missingMessage(key string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "no value for %q", key)

	if v == nil || len(v.sources) == 0 {
		sb.WriteString(" (no properties files are associated with this test)")
		return sb.String()
	}

	fmt.Fprintf(&sb, "; set it in %s, %s, or the environment as %s%s",
		filepath.Base(v.sources[0]),
		filepath.Base(v.sources[len(v.sources)-1]),
		envPrefix, strings.ToUpper(key))

	if keys := v.Keys(); len(keys) > 0 {
		fmt.Fprintf(&sb, ". Defined keys: %s", strings.Join(keys, ", "))
	} else {
		sb.WriteString(". No values are defined at all")
	}
	return sb.String()
}

// ParseProperties reads a Java-style properties file.
//
// Supported: key=value and key:value, # and ! comments, blank lines,
// backslash line continuation, and \n \t \r \\ \= \: escapes. Keys are
// case-sensitive; whitespace around the separator is trimmed.
func ParseProperties(content string) (map[string]string, error) {
	out := make(map[string]string)

	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "!") {
			continue
		}

		// A trailing backslash continues onto the next line.
		for strings.HasSuffix(trimmed, `\`) && !strings.HasSuffix(trimmed, `\\`) && i+1 < len(lines) {
			trimmed = strings.TrimSuffix(trimmed, `\`)
			i++
			trimmed += strings.TrimSpace(strings.TrimRight(lines[i], "\r"))
		}

		key, value, ok := splitProperty(trimmed)
		if !ok {
			return nil, fmt.Errorf("line %d is not key=value: %q", i+1, trimmed)
		}
		if key == "" {
			return nil, fmt.Errorf("line %d has an empty key", i+1)
		}
		out[key] = unescape(value)
	}

	return out, nil
}

// splitProperty finds the first unescaped = or : separator.
func splitProperty(line string) (key, value string, ok bool) {
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' {
			i++ // skip the escaped character
			continue
		}
		if line[i] == '=' || line[i] == ':' {
			return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
		}
	}
	return "", "", false
}

// unescape expands the escape sequences a properties file may contain.
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}

	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			sb.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			sb.WriteByte('\n')
		case 't':
			sb.WriteByte('\t')
		case 'r':
			sb.WriteByte('\r')
		default:
			// \\ \= \: and anything else: keep the character itself.
			sb.WriteByte(s[i])
		}
	}
	return sb.String()
}

// FormatProperties renders values as a properties file, sorted so that a
// regenerated file produces a readable diff rather than a reshuffle.
func FormatProperties(values map[string]string, header string) string {
	var sb strings.Builder
	if header != "" {
		for _, line := range strings.Split(strings.TrimRight(header, "\n"), "\n") {
			sb.WriteString("# " + line + "\n")
		}
		sb.WriteString("\n")
	}

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		fmt.Fprintf(&sb, "%s=%s\n", k, escapeValue(values[k]))
	}
	return sb.String()
}

func escapeValue(v string) string {
	r := strings.NewReplacer("\\", `\\`, "\n", `\n`, "\t", `\t`, "\r", `\r`)
	return r.Replace(v)
}

// parseIntValue is used by the JS int accessor.
func parseIntValue(key, raw string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("value %q is %q, which is not a whole number", key, raw)
	}
	return n, nil
}

// parseBoolValue accepts the spellings people actually write.
func parseBoolValue(key, raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "yes", "on", "1":
		return true, nil
	case "false", "no", "off", "0":
		return false, nil
	}
	return false, fmt.Errorf("value %q is %q, which is not a true/false value", key, raw)
}

// valuesHeader explains the file to whoever opens it next.
const valuesHeader = `Test inputs for %s — generated, but yours to edit.

These are the defaults everyone shares; keep them committed.
To change a value on this machine only, put it in %s
(gitignored), or set %s<KEY> in the environment.

Do not put passwords or tokens here. Store the secret in your password
manager and put its name here instead, then fetch it with
atr.fillSecret(target, {ref: values.get("..._ref")}).`

// SaveValues writes the committed properties file for a spec.
//
// It refuses to overwrite an existing file. The values file is the one part
// of a compiled test a human is expected to edit, and a recompile — which
// happens whenever the spec changes — must not silently discard the hosts,
// accounts and quantities somebody set up.
func SaveValues(specPath, baseURL, properties string) (string, error) {
	path := ValuesPath(specPath)

	parsed, err := ParseProperties(properties)
	if err != nil {
		return "", fmt.Errorf("the compiler produced an unreadable properties block: %w", err)
	}

	if _, err := os.Stat(path); err == nil {
		// Already there: add anything new, leave the rest alone.
		if _, err := MergeValues(specPath, properties); err != nil {
			return "", err
		}
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("checking %s: %w", path, err)
	}

	// Record the base URL so the test is runnable elsewhere without flags.
	if baseURL != "" {
		if _, ok := parsed["base_url"]; !ok {
			parsed["base_url"] = baseURL
		}
	}

	header := fmt.Sprintf(valuesHeader,
		filepath.Base(specPath), filepath.Base(OverridePath(specPath)), envPrefix)

	if err := os.WriteFile(path, []byte(FormatProperties(parsed, header)), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}

// MergeValues adds keys that are not already present, returning the names it
// added. Existing values are never changed: they are the ones a human tuned.
func MergeValues(specPath, properties string) ([]string, error) {
	path := ValuesPath(specPath)

	incoming, err := ParseProperties(properties)
	if err != nil {
		return nil, fmt.Errorf("unreadable properties block: %w", err)
	}
	if len(incoming) == 0 {
		return nil, nil
	}

	existingRaw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var added []string
	body := string(existingRaw)
	existing, err := ParseProperties(body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	var sb strings.Builder
	sb.WriteString(strings.TrimRight(body, "\n"))

	keys := make([]string, 0, len(incoming))
	for k := range incoming {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if _, ok := existing[k]; ok {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "%s=%s", k, escapeValue(incoming[k]))
		added = append(added, k)
	}
	if len(added) == 0 {
		return nil, nil
	}
	sb.WriteString("\n")

	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return nil, fmt.Errorf("writing %s: %w", path, err)
	}
	return added, nil
}
