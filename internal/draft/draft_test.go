package draft

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mayusuk/agent-loop/contract"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	sha1 = "1111111111111111111111111111111111111111"
	sha2 = "2222222222222222222222222222222222222222"
)

func input(s contract.Stage) contract.Input {
	in := contract.Input{
		ContractVersion: contract.Version,
		RunID:           "run-1",
		Stage:           s,
		Workspace:       contract.Workspace{Path: "/w", Branch: "agent/1", Head: sha1},
		Item:            contract.ItemRef{Ref: "1", Type: "feature"},
	}
	switch s {
	case contract.StageReview:
		in.Review = &contract.ReviewInput{
			Kind: contract.KindRound, Commit: sha1, BlockingThreshold: 4,
			RuleFindings: []string{"F1"}, RuleManual: []string{"MC-1"},
		}
	case contract.StageAddress:
		in.Address = &contract.AddressInput{
			Mode: contract.ModeFindings, AnswerFindings: []string{"F1", "F2"}, AnswerManual: []string{"MC-1"},
		}
	}
	return in
}

// convertJSON writes draft to a file and converts it.
func convertJSON(t *testing.T, in contract.Input, draft string) (contract.Result, string) {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "agent-draft.json")
	if err := os.WriteFile(p, []byte(draft), 0o644); err != nil {
		t.Fatal(err)
	}
	return Convert(in, p, out), out
}

// validate sets the head the stage would set and runs ValidateResult.
func validate(t *testing.T, in contract.Input, res contract.Result, out string) contract.Result {
	t.Helper()
	if res.Done != nil {
		if res.Done.Implement != nil {
			res.Done.Implement.Head = sha2
		}
		if res.Done.Address != nil {
			res.Done.Address.Head = sha2
		}
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	got, err := contract.ValidateResult(in, data, out)
	if err != nil {
		t.Fatalf("ValidateResult: %v\n%s", err, data)
	}
	return got
}

func readOut(t *testing.T, out, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const (
	blockedDraft      = `{"result":"blocked","blocked":{"reason":"needs #7 first","dependency":"7"}}`
	blockedNoDep      = `{"result":"blocked","blocked":{"reason":"needs a key"}}`
	needsOwnerDraft   = `{"result":"needs_owner","needs_owner":{"question":"Which API?"}}`
	failedDraft       = `{"result":"failed","failed":{"reason":"tests\nwould not run"}}`
	implementDone     = `{"result":"done","done":{"change":{"title":"Add login","body":"Adds login.\n\nTested."}}}`
	reviewDone        = `{"result":"done","done":{"summary":"Looks fine.","rulings":[{"finding":"F1","status":"addressed","note":"fixed in abc"}],"manual_rulings":[{"comment":"MC-1","ruling":"open"}],"new_findings":[{"score":4,"path":"api/login.go","line":88,"title":"Nil check","body":"Crashes when x is nil."},{"score":1,"path":null,"line":null,"title":"Docs","body":"Add docs."}]}}`
	addressDone       = `{"result":"done","done":{"replies":[{"finding":"F1","kind":"fixed","body":"Fixed."},{"finding":"F2","kind":"deferred","deferred_to":"61","body":"Later."}],"manual_replies":[{"comment":"MC-1","kind":"explained","body":"Because."}]}}`
	addressBaseUpdate = `{"result":"done","done":{"replies":[],"manual_replies":[]}}`
)

// TestT4AC1_EveryAllowedOutcomeConverts: for each stage and each allowed
// outcome, a draft converts to a result ValidateResult accepts.
func TestT4AC1_EveryAllowedOutcomeConverts(t *testing.T) {
	cases := []struct {
		stage contract.Stage
		draft string
		want  contract.Outcome
	}{
		{contract.StageImplement, implementDone, contract.OutcomeDone},
		{contract.StageImplement, blockedDraft, contract.OutcomeBlocked},
		{contract.StageImplement, needsOwnerDraft, contract.OutcomeNeedsOwner},
		{contract.StageImplement, failedDraft, contract.OutcomeFailed},
		{contract.StageReview, reviewDone, contract.OutcomeDone},
		{contract.StageReview, needsOwnerDraft, contract.OutcomeNeedsOwner},
		{contract.StageReview, failedDraft, contract.OutcomeFailed},
		{contract.StageAddress, addressDone, contract.OutcomeDone},
		{contract.StageAddress, blockedDraft, contract.OutcomeBlocked},
		{contract.StageAddress, needsOwnerDraft, contract.OutcomeNeedsOwner},
		{contract.StageAddress, failedDraft, contract.OutcomeFailed},
	}
	for _, c := range cases {
		t.Run(string(c.stage)+"/"+string(c.want), func(t *testing.T) {
			in := input(c.stage)
			res, out := convertJSON(t, in, c.draft)
			if res.Result != c.want {
				t.Fatalf("result = %q, want %q (%+v)", res.Result, c.want, res.Failed)
			}
			if c.want == contract.OutcomeFailed && strings.HasPrefix(res.Failed.Reason, FailedPrefix) {
				t.Fatalf("an agent's failed reason got the draft prefix: %q", res.Failed.Reason)
			}
			validate(t, in, res, out)
		})
	}
}

func TestT4AC1_BaseUpdateAddressConverts(t *testing.T) {
	in := input(contract.StageAddress)
	in.Address = &contract.AddressInput{Mode: contract.ModeBaseUpdate}
	res, out := convertJSON(t, in, addressBaseUpdate)
	validate(t, in, res, out)
}

func TestT4_ImplementDoneWritesTheBody(t *testing.T) {
	res, out := convertJSON(t, input(contract.StageImplement), implementDone)
	d := res.Done.Implement
	if d.Change.Title != "Add login" || d.Change.BodyFile != "change-body.md" || d.Head != "" {
		t.Fatalf("done = %+v", d)
	}
	if got := readOut(t, out, "change-body.md"); got != "Adds login.\n\nTested." {
		t.Fatalf("body = %q", got)
	}
	if res.RunID != "run-1" || res.ContractVersion != contract.Version {
		t.Fatalf("run_id %q, contract_version %q", res.RunID, res.ContractVersion)
	}
}

func TestT4_ReviewDoneFillsCommitAndKindAndFiles(t *testing.T) {
	in := input(contract.StageReview)
	in.Review.Kind = contract.KindOwnerPush
	res, out := convertJSON(t, in, reviewDone)
	d := res.Done.Review
	if d.Commit != sha1 || d.Kind != contract.KindOwnerPush {
		t.Fatalf("commit %q kind %q", d.Commit, d.Kind)
	}
	if d.Rulings[0].NoteFile != "ruling-1-note.md" || d.ManualRulings[0].NoteFile != "" {
		t.Fatalf("note files %+v %+v", d.Rulings, d.ManualRulings)
	}
	if d.NewFindings[1].BodyFile != "finding-2-body.md" || d.NewFindings[1].Path != nil || d.NewFindings[1].Line != nil {
		t.Fatalf("finding 2 = %+v", d.NewFindings[1])
	}
	for name, want := range map[string]string{
		"summary.md": "Looks fine.", "ruling-1-note.md": "fixed in abc",
		"finding-1-body.md": "Crashes when x is nil.", "finding-2-body.md": "Add docs.",
	} {
		if got := readOut(t, out, name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestT4_AddressDoneWritesReplies(t *testing.T) {
	res, out := convertJSON(t, input(contract.StageAddress), addressDone)
	d := res.Done.Address
	if d.Replies[1].DeferredTo == nil || *d.Replies[1].DeferredTo != "61" || d.Replies[0].DeferredTo != nil {
		t.Fatalf("replies = %+v", d.Replies)
	}
	for name, want := range map[string]string{
		"reply-1-body.md": "Fixed.", "reply-2-body.md": "Later.", "manual-reply-1-body.md": "Because.",
	} {
		if got := readOut(t, out, name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// TestFR18AC1_BlockedAndNeedsOwnerWriteFiles: blocked with and without a
// dependency, and needs_owner, give reason_file and question_file.
func TestFR18AC1_BlockedAndNeedsOwnerWriteFiles(t *testing.T) {
	in := input(contract.StageImplement)

	res, out := convertJSON(t, in, blockedDraft)
	if res.Blocked.ReasonFile != "blocked-reason.md" || res.Blocked.Dependency == nil || *res.Blocked.Dependency != "7" {
		t.Fatalf("blocked = %+v", res.Blocked)
	}
	if got := readOut(t, out, "blocked-reason.md"); got != "needs #7 first" {
		t.Fatalf("reason = %q", got)
	}

	for _, d := range []string{blockedNoDep,
		`{"result":"blocked","blocked":{"reason":"r","dependency":null}}`,
		`{"result":"blocked","blocked":{"reason":"r","dependency":"  "}}`} {
		res, out = convertJSON(t, in, d)
		if res.Blocked == nil || res.Blocked.Dependency != nil {
			t.Fatalf("%s: blocked = %+v", d, res.Blocked)
		}
		validate(t, in, res, out)
	}

	res, out = convertJSON(t, in, needsOwnerDraft)
	if res.NeedsOwner.QuestionFile != "needs-owner-question.md" {
		t.Fatalf("needs_owner = %+v", res.NeedsOwner)
	}
	if got := readOut(t, out, "needs-owner-question.md"); got != "Which API?" {
		t.Fatalf("question = %q", got)
	}
}

func TestT4_FailedReasonIsOneLineOfAtMost500(t *testing.T) {
	long := strings.Repeat("word ", 300)
	res, _ := convertJSON(t, input(contract.StageImplement), `{"result":"failed","failed":{"reason":"a\n  b `+long+`"}}`)
	r := res.Failed.Reason
	if strings.ContainsAny(r, "\r\n") || len([]rune(r)) > contract.MaxFailedReason || !strings.HasPrefix(r, "a b word") {
		t.Fatalf("reason = %q", r)
	}
	res, _ = convertJSON(t, input(contract.StageImplement), failedDraft)
	if res.Failed.Reason != "tests would not run" {
		t.Fatalf("reason = %q", res.Failed.Reason)
	}
}

// TestFR18AC3_BadDraftsFail: each problem gives failed with a reason that
// names it.
func TestFR18AC3_BadDraftsFail(t *testing.T) {
	big := strings.Repeat("x", contract.MaxTextFile+1)
	cases := []struct {
		name  string
		stage contract.Stage
		draft string
		want  string
	}{
		{"malformed JSON", contract.StageImplement, `{"result":"done",`, "not valid JSON"},
		{"not an object", contract.StageImplement, `["done"]`, "not a JSON object"},
		{"trailing data", contract.StageImplement, implementDone + `{}`, "not valid JSON"},
		{"no result", contract.StageImplement, `{"done":{}}`, "result is missing"},
		{"result not a string", contract.StageImplement, `{"result":1}`, "result is not a string"},
		{"unknown result", contract.StageImplement, `{"result":"maybe"}`, `result "maybe" is not allowed from implement`},
		{"blocked from review", contract.StageReview, blockedDraft, `result "blocked" is not allowed from review`},
		{"complete from implement", contract.StageImplement, `{"result":"complete","complete":{}}`, `result "complete" is not allowed`},
		{"missing outcome object", contract.StageImplement, `{"result":"done"}`, "missing property 'done'"},
		{"other outcome object", contract.StageImplement, `{"result":"failed","failed":{"reason":"x"},"blocked":{"reason":"y"}}`, "unknown fields blocked"},
		{"unknown top field", contract.StageImplement, `{"result":"failed","failed":{"reason":"x"},"extra":1}`, "unknown fields extra"},
		{"unknown nested field", contract.StageImplement, `{"result":"done","done":{"change":{"title":"t","body":"b","labels":[]}}}`, "done.change: has unknown fields labels"},
		{"case-variant key", contract.StageImplement, `{"result":"done","done":{"change":{"Title":"t","body":"b"}}}`, "done.change"},
		{"body_file instead of body", contract.StageImplement, `{"result":"done","done":{"change":{"title":"t","body_file":"b.md"}}}`, "done.change"},
		{"review field in implement", contract.StageImplement, `{"result":"done","done":{"change":{"title":"t","body":"b"},"summary":"s"}}`, "unknown fields summary"},
		{"blank title", contract.StageImplement, `{"result":"done","done":{"change":{"title":"  ","body":"b"}}}`, "done.change.title: is blank"},
		{"two-line title", contract.StageImplement, `{"result":"done","done":{"change":{"title":"a\nb","body":"b"}}}`, "done.change.title: is more than one line"},
		{"blank body", contract.StageImplement, `{"result":"done","done":{"change":{"title":"t","body":""}}}`, "done.change.body: is blank"},
		{"blank reason", contract.StageAddress, `{"result":"blocked","blocked":{"reason":"\n"}}`, "blocked.reason: is blank"},
		{"blank question", contract.StageReview, `{"result":"needs_owner","needs_owner":{"question":""}}`, "needs_owner.question: is blank"},
		{"bad score", contract.StageReview, `{"result":"done","done":{"summary":"s","new_findings":[{"score":6,"title":"t","body":"b"}]}}`, "done.new_findings[0].score"},
		{"fractional score", contract.StageReview, `{"result":"done","done":{"summary":"s","new_findings":[{"score":2.5,"title":"t","body":"b"}]}}`, "done.new_findings[0].score"},
		{"bad status", contract.StageReview, `{"result":"done","done":{"summary":"s","rulings":[{"finding":"F1","status":"fixed"}]}}`, "done.rulings[0].status"},
		{"bad reply kind", contract.StageAddress, `{"result":"done","done":{"replies":[{"finding":"F1","kind":"done","body":"b"}]}}`, "done.replies[0].kind"},
		{"reply without finding", contract.StageAddress, `{"result":"done","done":{"replies":[{"kind":"fixed","body":"b"}]}}`, "missing property 'finding'"},
		{"reply with comment", contract.StageAddress, `{"result":"done","done":{"replies":[{"finding":"F1","comment":"MC-1","kind":"fixed","body":"b"}]}}`, "done.replies[0]"},
		{"oversized body", contract.StageImplement, `{"result":"done","done":{"change":{"title":"t","body":"` + big + `"}}}`, "done.change.body is larger than 64 KiB"},
		{"oversized reply", contract.StageAddress, `{"result":"done","done":{"replies":[{"finding":"F1","kind":"fixed","body":"` + big + `"}]}}`, "done.replies[0].body is larger than 64 KiB"},
		{"oversized note", contract.StageReview, `{"result":"done","done":{"summary":"s","rulings":[{"finding":"F1","status":"open","note":"` + big + `"}]}}`, "done.rulings[0].note is larger than 64 KiB"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, out := convertJSON(t, input(c.stage), c.draft)
			if res.Result != contract.OutcomeFailed {
				t.Fatalf("result = %q, want failed", res.Result)
			}
			r := res.Failed.Reason
			if !strings.HasPrefix(r, FailedPrefix) || !strings.Contains(r, c.want) {
				t.Fatalf("reason = %q, want it to start with %q and contain %q", r, FailedPrefix, c.want)
			}
			if strings.ContainsAny(r, "\r\n") || len([]rune(r)) > contract.MaxFailedReason {
				t.Fatalf("reason is not one line of at most 500: %q", r)
			}
			if entries, _ := os.ReadDir(out); len(entries) != 0 {
				t.Fatalf("files were written for a bad draft: %v", entries)
			}
			validate(t, input(c.stage), res, out)
		})
	}
}

func TestFR18AC3_MissingAndUnreadableDrafts(t *testing.T) {
	in := input(contract.StageImplement)
	dir := t.TempDir()

	res := Convert(in, filepath.Join(dir, "none.json"), dir)
	if res.Failed == nil || !strings.Contains(res.Failed.Reason, "the agent wrote no draft at") {
		t.Fatalf("missing: %+v", res)
	}

	res = Convert(in, "", dir)
	if res.Failed == nil || !strings.Contains(res.Failed.Reason, "no draft path") {
		t.Fatalf("empty path: %+v", res)
	}

	res = Convert(in, dir, dir)
	if res.Failed == nil || !strings.Contains(res.Failed.Reason, "is not a regular file") {
		t.Fatalf("directory: %+v", res)
	}

	real := filepath.Join(dir, "real.json")
	if err := os.WriteFile(real, []byte(implementDone), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	res = Convert(in, link, dir)
	if res.Failed == nil || !strings.Contains(res.Failed.Reason, "is not a regular file") {
		t.Fatalf("symlink: %+v", res)
	}

	big := filepath.Join(dir, "big.json")
	if err := os.WriteFile(big, []byte(`{"result":"failed","failed":{"reason":"`+strings.Repeat("x", MaxDraft)+`"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	res = Convert(in, big, dir)
	if res.Failed == nil || !strings.Contains(res.Failed.Reason, "larger than 1048576 bytes") {
		t.Fatalf("big: %+v", res)
	}

	res, _ = convertJSON(t, in, "{\"result\":\"failed\",\"failed\":{\"reason\":\"\xff\"}}")
	if res.Failed == nil || !strings.Contains(res.Failed.Reason, "not UTF-8") {
		t.Fatalf("not UTF-8: %+v", res)
	}
}

// TestT4AC4_HandlerOwnedFieldsAreRejected: run_id, commit, head and the
// other fields the handler owns are rejected, never copied.
func TestT4AC4_HandlerOwnedFieldsAreRejected(t *testing.T) {
	cases := []struct {
		stage contract.Stage
		draft string
		want  string
	}{
		{contract.StageImplement, `{"run_id":"evil","result":"done","done":{"change":{"title":"t","body":"b"}}}`, "run_id is set, but the handler fills it in"},
		{contract.StageImplement, `{"contract_version":"9.0","result":"failed","failed":{"reason":"x"}}`, "contract_version is set"},
		{contract.StageImplement, `{"result":"done","done":{"head":"` + sha2 + `","change":{"title":"t","body":"b"}}}`, "done.head is set, but the handler fills it in"},
		{contract.StageAddress, `{"result":"done","done":{"head":"` + sha2 + `","replies":[]}}`, "done.head is set"},
		{contract.StageReview, `{"result":"done","done":{"commit":"` + sha2 + `","summary":"s"}}`, "done.commit is set"},
		{contract.StageReview, `{"result":"done","done":{"kind":"owner_push","summary":"s"}}`, "done.kind is set"},
	}
	for _, c := range cases {
		res, _ := convertJSON(t, input(c.stage), c.draft)
		if res.Failed == nil || !strings.Contains(res.Failed.Reason, c.want) {
			t.Errorf("%s: got %+v, want failed containing %q", c.draft, res, c.want)
		}
		if res.RunID != "run-1" {
			t.Errorf("%s: run_id = %q", c.draft, res.RunID)
		}
	}
}

func TestT4_ExistingOutFileIsReplacedNotFollowed(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(out, "change-body.md")); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "d.json")
	if err := os.WriteFile(p, []byte(implementDone), 0o644); err != nil {
		t.Fatal(err)
	}
	in := input(contract.StageImplement)
	res := Convert(in, p, out)
	validate(t, in, res, out)
	if b, _ := os.ReadFile(target); string(b) != "keep" {
		t.Fatalf("wrote through the symlink: %q", b)
	}
	if fi, err := os.Lstat(filepath.Join(out, "change-body.md")); err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("change-body.md is not a regular file: %v %v", fi, err)
	}
}

func TestT4_SchemaIsValidJSONWithStageDefs(t *testing.T) {
	var doc struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(Schema(), &doc); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"implement", "review", "address", "implement_done", "review_done", "address_done", "blocked", "needs_owner", "failed"} {
		if _, err := def(name); err != nil {
			t.Errorf("def %s: %v", name, err)
		}
	}
}

// TestT4_StageSchemasMatchAllowedOutcomes checks the per-stage oneOf
// schemas for skill authors agree with Parse.
func TestT4_StageSchemasMatchAllowedOutcomes(t *testing.T) {
	drafts := map[contract.Outcome]map[contract.Stage]string{
		contract.OutcomeDone:       {contract.StageImplement: implementDone, contract.StageReview: reviewDone, contract.StageAddress: addressDone},
		contract.OutcomeBlocked:    {contract.StageImplement: blockedDraft, contract.StageReview: blockedDraft, contract.StageAddress: blockedDraft},
		contract.OutcomeNeedsOwner: {contract.StageImplement: needsOwnerDraft, contract.StageReview: needsOwnerDraft, contract.StageAddress: needsOwnerDraft},
		contract.OutcomeFailed:     {contract.StageImplement: failedDraft, contract.StageReview: failedDraft, contract.StageAddress: failedDraft},
	}
	for o, byStage := range drafts {
		for s, d := range byStage {
			sch, err := def(string(s))
			if err != nil {
				t.Fatal(err)
			}
			inst, err := unmarshalInstance(d)
			if err != nil {
				t.Fatal(err)
			}
			schemaOK := sch.Validate(inst) == nil
			_, parseErr := Parse(s, []byte(d))
			if schemaOK != (parseErr == nil) || schemaOK != contract.Allowed(s, o) {
				t.Errorf("%s %s: schema ok %v, parse err %v, allowed %v", s, o, schemaOK, parseErr, contract.Allowed(s, o))
			}
		}
	}
}

func unmarshalInstance(s string) (any, error) {
	return jsonschema.UnmarshalJSON(strings.NewReader(s))
}

// TestT4D4_EmptyOrBlankFindingPathIsNotGiven: an empty or blank finding
// path counts as not given (work plan D4): the review converts and the
// finding has no path.
func TestT4D4_EmptyOrBlankFindingPathIsNotGiven(t *testing.T) {
	in := input(contract.StageReview)
	for _, p := range []string{`""`, `"   "`, "\"\\t\\n\""} {
		d := `{"result":"done","done":{"summary":"s","rulings":[{"finding":"F1","status":"open"}],"manual_rulings":[{"comment":"MC-1","ruling":"open"}],"new_findings":[{"score":2,"path":` + p + `,"line":null,"title":"t","body":"b"}]}}`
		res, out := convertJSON(t, in, d)
		if res.Result != contract.OutcomeDone {
			t.Fatalf("path %s: result = %q (%+v)", p, res.Result, res.Failed)
		}
		if f := res.Done.Review.NewFindings[0]; f.Path != nil {
			t.Fatalf("path %s: finding path = %q, want none", p, *f.Path)
		}
		validate(t, in, res, out)
	}

	// A line with a blank path is a line with no path, which the contract
	// rejects when run.Finish validates the result.
	d := `{"result":"done","done":{"summary":"s","rulings":[{"finding":"F1","status":"open"}],"manual_rulings":[{"comment":"MC-1","ruling":"open"}],"new_findings":[{"score":2,"path":" ","line":3,"title":"t","body":"b"}]}}`
	res, out := convertJSON(t, in, d)
	if res.Result != contract.OutcomeDone || res.Done.Review.NewFindings[0].Path != nil {
		t.Fatalf("blank path with line: %+v", res)
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := contract.ValidateResult(in, data, out); err == nil || !strings.Contains(err.Error(), "line needs a path") {
		t.Fatalf("ValidateResult err = %v, want line needs a path", err)
	}
}

// TestT4D4_GivenFindingPathIsKept: a finding path with text is copied as is.
func TestT4D4_GivenFindingPathIsKept(t *testing.T) {
	res, _ := convertJSON(t, input(contract.StageReview), reviewDone)
	if p := res.Done.Review.NewFindings[0].Path; p == nil || *p != "api/login.go" {
		t.Fatalf("finding path = %v", p)
	}
}

func convertWith(t *testing.T, in contract.Input, draft string, fix Fix) (contract.Result, string) {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	os.Mkdir(out, 0o755)
	p := filepath.Join(dir, "agent-draft.json")
	os.WriteFile(p, []byte(draft), 0o644)
	return ConvertWith(in, p, out, fix), out
}

func TestT6D8_FixChangesTheDoneDraftBeforeItIsWritten(t *testing.T) {
	calls := 0
	res, out := convertWith(t, input(contract.StageReview), reviewDone, func(d *Done) error {
		calls++
		d.NewFindings[0].Path = nil
		d.NewFindings[0].Line = nil
		d.NewFindings[0].Body = "moved\n\n" + d.NewFindings[0].Body
		return nil
	})
	if calls != 1 {
		t.Fatalf("fix called %d times", calls)
	}
	f := res.Done.Review.NewFindings[0]
	if f.Path != nil || f.Line != nil {
		t.Errorf("finding 1 = %+v, want no location", f)
	}
	if got := readOut(t, out, f.BodyFile); got != "moved\n\nCrashes when x is nil." {
		t.Errorf("body = %q", got)
	}
}

func TestT6D8_FixErrorIsFailedWithoutTheDraftPrefix(t *testing.T) {
	res, _ := convertWith(t, input(contract.StageReview), reviewDone, func(*Done) error {
		return errors.New("git cat-file: boom")
	})
	if res.Result != contract.OutcomeFailed || res.Failed.Reason != "git cat-file: boom" {
		t.Fatalf("result = %+v", res)
	}
}

func TestT6D8_FixIsNotCalledForOtherOutcomes(t *testing.T) {
	res, _ := convertWith(t, input(contract.StageReview), `{"result":"failed","failed":{"reason":"no"}}`, func(*Done) error {
		t.Error("fix called")
		return nil
	})
	if res.Result != contract.OutcomeFailed {
		t.Fatalf("result = %s", res.Result)
	}
}
