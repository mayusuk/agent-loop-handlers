// Package draft is the agent's draft: the JSON file the agent writes at
// the end of a stage run, with its outcome and its text inline, and the
// conversion of that draft into a result.json result (plan D1, T4). The
// format is documented in docs/agent-draft.md and defined by the embedded
// agent-draft.schema.json.
package draft

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/mayusuk/agent-loop/contract"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// MaxDraft is the largest draft file accepted, in bytes (work plan D3).
const MaxDraft = 1 << 20

// SchemaURL is the $id of the embedded schema.
const SchemaURL = "https://github.com/mayusuk/agent-loop-handlers/agent-draft.schema.json"

//go:embed agent-draft.schema.json
var schemaJSON []byte

// Schema returns the draft's JSON Schema.
func Schema() []byte { return bytes.Clone(schemaJSON) }

// Draft is a parsed draft. Exactly one of the outcome fields is set: the
// one Result names.
type Draft struct {
	Result     contract.Outcome `json:"result"`
	Done       *Done            `json:"done,omitempty"`
	Blocked    *Blocked         `json:"blocked,omitempty"`
	NeedsOwner *NeedsOwner      `json:"needs_owner,omitempty"`
	Failed     *Failed          `json:"failed,omitempty"`
}

// Done is the done object of every stage. The schema allows only the
// stage's own fields: Change for implement; Summary, Rulings,
// ManualRulings and NewFindings for review; Replies and ManualReplies for
// address.
type Done struct {
	Change        *Change        `json:"change,omitempty"`
	Summary       string         `json:"summary,omitempty"`
	Rulings       []Ruling       `json:"rulings,omitempty"`
	ManualRulings []ManualRuling `json:"manual_rulings,omitempty"`
	NewFindings   []NewFinding   `json:"new_findings,omitempty"`
	Replies       []Reply        `json:"replies,omitempty"`
	ManualReplies []ManualReply  `json:"manual_replies,omitempty"`
}

// Change is implement's new change.
type Change struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Ruling is review's ruling on an earlier finding.
type Ruling struct {
	Finding string                 `json:"finding"`
	Status  contract.FindingStatus `json:"status"`
	Note    *string                `json:"note,omitempty"`
}

// ManualRuling is review's ruling on a manual comment.
type ManualRuling struct {
	Comment string                     `json:"comment"`
	Ruling  contract.ManualRulingValue `json:"ruling"`
	Note    *string                    `json:"note,omitempty"`
}

// NewFinding is a finding the review reports.
type NewFinding struct {
	Score int     `json:"score"`
	Path  *string `json:"path,omitempty"`
	Line  *int    `json:"line,omitempty"`
	Title string  `json:"title"`
	Body  string  `json:"body"`
}

// Reply is address's reply to a finding.
type Reply struct {
	Finding    string             `json:"finding"`
	Kind       contract.ReplyKind `json:"kind"`
	DeferredTo *string            `json:"deferred_to,omitempty"`
	Body       string             `json:"body"`
}

// ManualReply is address's reply to a manual comment.
type ManualReply struct {
	Comment    string             `json:"comment"`
	Kind       contract.ReplyKind `json:"kind"`
	DeferredTo *string            `json:"deferred_to,omitempty"`
	Body       string             `json:"body"`
}

// Blocked is the blocked object.
type Blocked struct {
	Reason     string  `json:"reason"`
	Dependency *string `json:"dependency,omitempty"`
}

// NeedsOwner is the needs_owner object.
type NeedsOwner struct {
	Question string `json:"question"`
}

// Failed is the failed object.
type Failed struct {
	Reason string `json:"reason"`
}

// handlerOwned are fields the handler fills in itself. A draft that sets
// one is rejected with a reason naming it (work plan D1).
var (
	handlerOwnedTop  = []string{"contract_version", "run_id"}
	handlerOwnedDone = []string{"commit", "head", "kind"}
)

var (
	compileOnce sync.Once
	compiler    *jsonschema.Compiler
	compileErr  error
	schemas     sync.Map // def name -> *jsonschema.Schema
)

// def returns the compiled schema for one $defs entry.
func def(name string) (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
		if err != nil {
			compileErr = err
			return
		}
		c := jsonschema.NewCompiler()
		c.DefaultDraft(jsonschema.Draft2020)
		compileErr = c.AddResource(SchemaURL, doc)
		compiler = c
	})
	if compileErr != nil {
		return nil, compileErr
	}
	if s, ok := schemas.Load(name); ok {
		return s.(*jsonschema.Schema), nil
	}
	s, err := compiler.Compile(SchemaURL + "#/$defs/" + name)
	if err != nil {
		return nil, err
	}
	schemas.Store(name, s)
	return s, nil
}

// Parse decodes and checks a draft for stage s. The error names the
// problem and the field (FR-18 AC3).
func Parse(s contract.Stage, data []byte) (Draft, error) {
	if len(data) > MaxDraft {
		return Draft{}, fmt.Errorf("the draft is larger than %d bytes", MaxDraft)
	}
	if !utf8.Valid(data) {
		return Draft{}, errors.New("the draft is not UTF-8")
	}
	var top map[string]json.RawMessage
	if t := bytes.TrimSpace(data); len(t) == 0 || t[0] != '{' || json.Unmarshal(data, &top) != nil {
		var syn *json.SyntaxError
		if err := json.Unmarshal(data, new(any)); errors.As(err, &syn) {
			return Draft{}, fmt.Errorf("the draft is not valid JSON: %v (at byte %d)", syn, syn.Offset)
		} else if err != nil {
			return Draft{}, fmt.Errorf("the draft is not valid JSON: %v", err)
		}
		return Draft{}, errors.New("the draft is not a JSON object")
	}
	raw, ok := top["result"]
	if !ok {
		return Draft{}, errors.New("result is missing")
	}
	var outcome contract.Outcome
	if json.Unmarshal(raw, &outcome) != nil {
		return Draft{}, errors.New("result is not a string")
	}
	if !contract.Allowed(s, outcome) {
		return Draft{}, fmt.Errorf("result %q is not allowed from %s", outcome, s)
	}
	for _, k := range handlerOwnedTop {
		if _, ok := top[k]; ok {
			return Draft{}, fmt.Errorf("%s is set, but the handler fills it in", k)
		}
	}
	if outcome == contract.OutcomeDone {
		var done map[string]json.RawMessage
		if json.Unmarshal(top["done"], &done) == nil {
			for _, k := range handlerOwnedDone {
				if _, ok := done[k]; ok {
					return Draft{}, fmt.Errorf("done.%s is set, but the handler fills it in", k)
				}
			}
		}
	}

	name := string(outcome)
	if outcome == contract.OutcomeDone {
		name = string(s) + "_done"
	}
	sch, err := def(name)
	if err != nil {
		return Draft{}, fmt.Errorf("internal error: draft schema: %v", err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return Draft{}, fmt.Errorf("the draft is not valid JSON: %v", err)
	}
	if err := sch.Validate(inst); err != nil {
		var ve *jsonschema.ValidationError
		if errors.As(err, &ve) {
			return Draft{}, schemaError(ve)
		}
		return Draft{}, err
	}

	var d Draft
	if err := json.Unmarshal(data, &d); err != nil {
		return Draft{}, fmt.Errorf("the draft does not decode: %v", err)
	}
	return d, nil
}

// schemaError reports the first leaf of a schema validation error as one
// line naming the field.
func schemaError(ve *jsonschema.ValidationError) error {
	for len(ve.Causes) > 0 {
		ve = ve.Causes[0]
	}
	field := fieldPath(ve.InstanceLocation)
	var msg string
	switch k := ve.ErrorKind.(type) {
	case *kind.Pattern:
		// The only patterns are the text and line rules (work plan D4);
		// the text itself is not repeated.
		msg = "is blank"
		if strings.ContainsAny(k.Got, "\r\n") && strings.TrimSpace(k.Got) != "" {
			msg = "is more than one line"
		}
	case *kind.AdditionalProperties:
		msg = "has unknown fields " + strings.Join(k.Properties, ", ")
	default:
		// The library's own English message for the leaf, without
		// importing golang.org/x/text for a printer (review R3).
		msg = "fails the schema"
		if out := ve.BasicOutput(); out.Error != nil {
			msg = out.Error.String()
		}
	}
	if field == "" {
		return errors.New(msg)
	}
	return fmt.Errorf("%s: %s", field, msg)
}

// fieldPath turns a JSON pointer's tokens into done.replies[0].body form.
func fieldPath(tokens []string) string {
	var b strings.Builder
	for _, t := range tokens {
		if _, err := strconv.Atoi(t); err == nil {
			b.WriteString("[" + t + "]")
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(t)
	}
	return b.String()
}
