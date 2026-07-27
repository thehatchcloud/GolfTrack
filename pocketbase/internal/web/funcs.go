package web

import (
	"encoding/json"
	"html/template"
	"strconv"
	"time"

	"github.com/thehatchcloud/golftrack/pocketbase/internal/hooks/domain/scoring"
)

// The template functions, which exist to keep the ported markup close to the
// Django originals. Django template filters (`|date:"M j, Y"`, `|default:"—"`)
// and its `{% if x is not None %}` chains have no Go equivalent, so the few
// that the pages actually use are provided here rather than being unrolled into
// the templates as nested `{{if}}` blocks.
var templateFuncs = map[string]any{
	"toPar":     toPar,
	"toParInt":  scoring.FormatRelativeToPar,
	"orDash":    orDash,
	"shortDate": shortDate,
	"jsonBlob":  jsonBlob,
}

// toPar renders a round's score relative to par the way every Django template
// does it — "E" at level, an explicit "+" over, and an em dash when the round
// has no total yet.
//
// The argument is `any` because RoundOut types the three totals that way: they
// are `null` until a round is completed (API.md gap 2), and a nil here is
// exactly that null.
func toPar(value any) string {
	number, ok := asInt(value)
	if !ok {
		return "—"
	}
	return scoring.FormatRelativeToPar(number)
}

// orDash is Django's `|default:"—"` over the same nullable totals.
func orDash(value any) string {
	number, ok := asInt(value)
	if !ok {
		return "—"
	}
	return strconv.Itoa(number)
}

func asInt(value any) (int, bool) {
	switch typed := value.(type) {
	case nil:
		return 0, false
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}

// pocketBaseTimeLayouts are the shapes a PocketBase datetime string arrives in.
// types.DateTime.String() renders "2006-01-02 15:04:05.000Z"; the second layout
// covers a value that lost its fractional seconds somewhere.
var pocketBaseTimeLayouts = []string{
	"2006-01-02 15:04:05.000Z",
	"2006-01-02 15:04:05Z",
}

// shortDate is Django's `|date:"M j, Y"`. An unparseable or empty value renders
// as an em dash rather than as a raw timestamp.
func shortDate(value any) string {
	raw, ok := value.(string)
	if !ok || raw == "" {
		return "—"
	}

	for _, layout := range pocketBaseTimeLayouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.Format("Jan 2, 2006")
		}
	}

	return "—"
}

// jsonBlob renders a value as JSON for a `<script type="application/json">`
// bootstrap block — the Go counterpart of Django's `json_script`, which is how
// the play page hands the round to Alpine.
//
// The return type is template.JS rather than template.HTML because the blob
// sits inside a script element: html/template escapes an HTML-typed value into
// a *JavaScript string literal* there, which would hand Alpine a quoted string
// instead of an object.
//
// encoding/json escapes `<`, `>` and `&` as <, > and & by
// default, so the result cannot close the surrounding script element. That is
// what makes marking it safe defensible; everything else in these templates
// goes through normal escaping.
func jsonBlob(value any) (template.JS, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	return template.JS(encoded), nil
}
