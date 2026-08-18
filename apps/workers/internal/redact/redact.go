// Package redact removes the values nobody meant to send us, before anything
// is stored (ENT-231, §25; OWASP LLM02).
//
// # WHY THIS RUNS IN THE GATEWAY AND NOT IN core-api
//
// "Redaction runs before storage" is only a property if there is no ordering
// in which storage happens first. Redacting in the process that writes the row
// leaves that ordering available: somebody writes the insert, somebody else
// adds the redaction, and for one release the two are the wrong way round with
// nothing to notice. Redacting here means the unredacted form never crosses
// the network to the process that holds the database, so the wrong order is
// not expressible.
//
// # WHAT IT LOOKS FOR, AND WHAT IT DELIBERATELY DOES NOT
//
// It looks for a small set of patterns that are unambiguous, high-value and
// cheap to recognise: bearer tokens and API keys, private key blocks, email
// addresses, IBANs, and long digit runs that read as card numbers. Each is a
// value that arrives by accident in a helpdesk ticket or a log line, and none
// of them is something an obligation is ever derived from.
//
// It deliberately does not attempt names, addresses, dates of birth or free
// prose. A redactor that tried would be wrong in both directions at once: it
// would miss most of what it aimed at, and it would destroy the observations
// this product exists to reason over. "Our support team handles subject access
// requests by email" is exactly the sentence a finding needs, and a redactor
// keen on personal data would take it away.
//
// So the scope is narrow and stated: secrets and direct identifiers that are
// never evidence, removed; everything else stored, under RLS, in a table the
// customer can read and erase. The second half is the control for the rest,
// and it is a better one than an over-eager regular expression.
//
// # REPLACEMENT, NOT DELETION
//
// A redacted value becomes a marker naming what it was. Deleting it would make
// a redacted observation indistinguishable from one where the field was empty,
// and "we saw a card number here and removed it" is a materially different
// statement from "there was nothing here".
package redact

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Marker is what replaces a redacted value.
const Marker = "[redacted]"

// rule is one pattern and what it is called.
//
// The name is not used in the output today, and the type keeps it anyway,
// because the moment somebody wants counts per kind on the fetch record this
// is where they come from, and adding the field later would mean touching
// every entry.
type rule struct {
	name    string
	pattern *regexp.Regexp
}

// rules are applied in order, and order matters exactly once: the private key
// block has to run before anything that might match inside it.
var rules = []rule{
	{
		name: "private-key-block",
		// PEM blocks, whatever the algorithm. Non-greedy so two blocks in one
		// document do not collapse into one match with everything between them
		// removed, which would take real content with it.
		pattern: regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
	},
	{
		name: "bearer-token",
		// `Bearer` followed by anything token-shaped. Case-insensitive because
		// the header is written every way there is.
		pattern: regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._\-+/=]{12,}`),
	},
	{
		name: "assigned-secret",
		// `api_key: value`, `password = value`, `"token": "value"`. The key
		// name is kept and only the value goes, because a field called
		// `password` being present is itself worth seeing.
		pattern: regexp.MustCompile(
			`(?i)("?\b(?:api[_-]?key|secret|password|passwd|token|access[_-]?token|client[_-]?secret)"?\s*[:=]\s*"?)([^"\s,}]{6,})`),
	},
	{
		name: "email",
		// Deliberately plain. An address-parsing regular expression that
		// handles every RFC 5322 case is famously enormous and would still
		// need this one for the common shape.
		pattern: regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`),
	},
	{
		name: "iban",
		// Two letters, two check digits, then up to thirty alphanumerics.
		pattern: regexp.MustCompile(`\b[A-Z]{2}[0-9]{2}[A-Z0-9]{11,30}\b`),
	},
	{
		name: "card-number",
		// Thirteen to nineteen digits, optionally in groups. Not Luhn-checked:
		// a number that fails Luhn and looks exactly like a card number is
		// still not something to store.
		pattern: regexp.MustCompile(`\b(?:\d[ \-]?){13,19}\b`),
	},
}

// secretKeyPattern names the JSON fields whose value is never evidence.
//
// Matched on the whole key rather than as a substring, so a field called
// `token_count` is not mistaken for a token. `_` and `-` spellings both,
// because a document assembled from two systems carries both.
var secretKeyPattern = regexp.MustCompile(
	`(?i)^(api[_-]?key|apikey|secret|client[_-]?secret|password|passwd|pwd|token|access[_-]?token|refresh[_-]?token|authorization|auth[_-]?token|private[_-]?key|credential)$`)

func secretKey(name string) bool { return secretKeyPattern.MatchString(name) }

// Result is redacted text and how many values were replaced.
type Result struct {
	Text string
	// Count is a fact and zero is a real answer: it says the redactor ran and
	// found nothing, which is a different statement from not having run.
	Count int
}

// Text redacts a plain string.
func Text(input string) Result {
	out := input
	count := 0
	for _, r := range rules {
		if r.name == "assigned-secret" {
			// This one keeps its first capture group, the key name, so the
			// output reads `api_key: [redacted]` rather than `[redacted]`.
			out = r.pattern.ReplaceAllStringFunc(out, func(match string) string {
				groups := r.pattern.FindStringSubmatch(match)
				if len(groups) < 3 {
					return match
				}
				count++
				return groups[1] + Marker
			})
			continue
		}
		out = r.pattern.ReplaceAllStringFunc(out, func(string) string {
			count++
			return Marker
		})
	}
	return Result{Text: out, Count: count}
}

// JSON redacts every string inside a JSON document, leaving its shape intact.
//
// # WHY THE SHAPE SURVIVES
//
// The alternative, running Text over the serialised document, is simpler and
// wrong: a pattern spanning a quote or a brace would produce output that is no
// longer JSON, and the observation would then be unparseable rather than
// redacted. Walking the decoded value means a redaction can only ever replace
// the contents of one string.
//
// Input that is not JSON is redacted as text and returned as text. That is the
// honest fallback: an MCP tool returning a plain string is ordinary, and
// refusing it would turn a working connection into a broken one over a
// formatting preference.
func JSON(input string) Result {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return Result{Text: input}
	}

	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return Text(input)
	}

	count := 0
	walked := walk(decoded, &count)

	encoded, err := json.Marshal(walked)
	if err != nil {
		// Unreachable in practice: what came out of Unmarshal re-marshals.
		// Falling back to the text path rather than returning the input keeps
		// the guarantee that nothing leaves here unredacted.
		return Text(input)
	}
	return Result{Text: string(encoded), Count: count}
}

// walk redacts strings anywhere in a decoded JSON value.
//
// Map KEYS are left alone. A key is a field name chosen by whoever wrote the
// tool, and redacting one would rename a field, which changes the meaning of a
// document rather than removing a value from it.
func walk(value any, count *int) any {
	switch typed := value.(type) {
	case string:
		result := Text(typed)
		*count += result.Count
		return result.Text
	case []any:
		for i, item := range typed {
			typed[i] = walk(item, count)
		}
		return typed
	case map[string]any:
		for key, item := range typed {
			// A VALUE UNDER A SECRET-SOUNDING KEY GOES WHOLE, whatever it
			// looks like.
			//
			// The `assigned-secret` pattern above only fires on `key: value`
			// written inside one string, which is what a log line or a ticket
			// body looks like. In a JSON document the key and the value are
			// separate nodes, so by the time the value is walked the key is out
			// of scope and `sk_live_9f8a...` is just an unremarkable string.
			// Found by a test that expected `{"api_key": "..."}` to be
			// redacted and watched it come back untouched.
			if text, isString := item.(string); isString && text != "" && secretKey(key) {
				*count++
				typed[key] = Marker
				continue
			}
			typed[key] = walk(item, count)
		}
		return typed
	default:
		// Numbers, booleans and null. A number cannot carry a bearer token,
		// and a card number arriving as a JSON number rather than a string is
		// a case worth naming: it is left alone here, and the reason is that
		// treating every long integer as a card number would redact row counts
		// and timestamps out of every observation this product stores.
		return value
	}
}
