package draft

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mayusuk/agent-loop-handlers/internal/run"
	"github.com/mayusuk/agent-loop/contract"
)

// FailedPrefix starts the reason of every failed result Convert returns for
// a problem with the draft, so the log tells it from a failure the agent
// reported itself (work plan D6).
const FailedPrefix = "draft: "

// Convert reads the draft at draftPath for the input's stage and turns it
// into a result. Each text is written to a file in outDir and the matching
// *_file field set; run_id, contract_version and review's commit and kind
// come from the input. done.head is left empty for implement and address:
// the stage sets it after its push (work plan D2).
//
// Any problem with the draft gives a failed result whose reason starts
// with FailedPrefix and names it (FR-18 AC3). A failed draft gives a
// failed result with the agent's reason, folded to one line of at most
// contract.MaxFailedReason characters. The result still goes through
// contract.ValidateResult in run.Finish.
func Convert(in contract.Input, draftPath, outDir string) contract.Result {
	return ConvertWith(in, draftPath, outDir, nil)
}

// Fix changes a parsed done draft before its texts are written, for
// example to adjust review's new findings (plan D8). An error from it gives
// a failed result with the error as the reason, without FailedPrefix,
// since it is not a problem with the draft.
type Fix func(*Done) error

// ConvertWith is Convert with fix applied to a done draft, if fix is not
// nil.
func ConvertWith(in contract.Input, draftPath, outDir string, fix Fix) contract.Result {
	res, err := convert(in, draftPath, outDir, fix)
	var fe fixError
	switch {
	case errors.As(err, &fe):
		res = run.Failed(fe.err.Error())
	case err != nil:
		res = run.Failed(FailedPrefix + err.Error())
	}
	res.ContractVersion = contract.Version
	res.RunID = in.RunID
	return res
}

// fixError marks an error from a Fix.
type fixError struct{ err error }

func (e fixError) Error() string { return e.err.Error() }

func convert(in contract.Input, draftPath, outDir string, fix Fix) (contract.Result, error) {
	data, err := readDraft(draftPath)
	if err != nil {
		return contract.Result{}, err
	}
	d, err := Parse(in.Stage, data)
	if err != nil {
		return contract.Result{}, err
	}
	w := &texts{}
	res := contract.Result{Result: d.Result}
	switch d.Result {
	case contract.OutcomeFailed:
		return run.Failed(d.Failed.Reason), nil
	case contract.OutcomeBlocked:
		res.Blocked = &contract.Blocked{
			ReasonFile: w.add("blocked-reason.md", "blocked.reason", d.Blocked.Reason),
			Dependency: given(d.Blocked.Dependency),
		}
	case contract.OutcomeNeedsOwner:
		res.NeedsOwner = &contract.NeedsOwner{
			QuestionFile: w.add("needs-owner-question.md", "needs_owner.question", d.NeedsOwner.Question),
		}
	case contract.OutcomeDone:
		if fix != nil {
			if err := fix(d.Done); err != nil {
				return contract.Result{}, fixError{err}
			}
		}
		done, err := convertDone(in, d.Done, w)
		if err != nil {
			return contract.Result{}, err
		}
		res.Done = done
	default:
		return contract.Result{}, fmt.Errorf("result %q is not allowed from %s", d.Result, in.Stage)
	}
	if err := w.write(outDir); err != nil {
		return contract.Result{}, err
	}
	return res, nil
}

func convertDone(in contract.Input, d *Done, w *texts) (*contract.Done, error) {
	switch in.Stage {
	case contract.StageImplement:
		return &contract.Done{Implement: &contract.ImplementDone{
			Change: &contract.NewChange{
				Title:    d.Change.Title,
				BodyFile: w.add("change-body.md", "done.change.body", d.Change.Body),
			},
		}}, nil

	case contract.StageReview:
		if in.Review == nil {
			return nil, errors.New("the input has no review object")
		}
		rd := &contract.ReviewDone{
			Commit:      in.Review.Commit,
			Kind:        in.Review.Kind,
			SummaryFile: w.add("summary.md", "done.summary", d.Summary),
		}
		for i, r := range d.Rulings {
			rd.Rulings = append(rd.Rulings, contract.Ruling{
				Finding:  r.Finding,
				Status:   r.Status,
				NoteFile: w.optional(fmt.Sprintf("ruling-%d-note.md", i+1), fmt.Sprintf("done.rulings[%d].note", i), r.Note),
			})
		}
		for i, r := range d.ManualRulings {
			rd.ManualRulings = append(rd.ManualRulings, contract.ManualRuling{
				Comment:  r.Comment,
				Ruling:   r.Ruling,
				NoteFile: w.optional(fmt.Sprintf("manual-ruling-%d-note.md", i+1), fmt.Sprintf("done.manual_rulings[%d].note", i), r.Note),
			})
		}
		for i, f := range d.NewFindings {
			rd.NewFindings = append(rd.NewFindings, contract.NewFinding{
				Score:    f.Score,
				Path:     given(f.Path),
				Line:     f.Line,
				Title:    f.Title,
				BodyFile: w.add(fmt.Sprintf("finding-%d-body.md", i+1), fmt.Sprintf("done.new_findings[%d].body", i), f.Body),
			})
		}
		return &contract.Done{Review: rd}, nil

	case contract.StageAddress:
		ad := &contract.AddressDone{}
		for i, r := range d.Replies {
			ad.Replies = append(ad.Replies, contract.Reply{
				Finding:    r.Finding,
				Kind:       r.Kind,
				DeferredTo: given(r.DeferredTo),
				BodyFile:   w.add(fmt.Sprintf("reply-%d-body.md", i+1), fmt.Sprintf("done.replies[%d].body", i), r.Body),
			})
		}
		for i, r := range d.ManualReplies {
			ad.ManualReplies = append(ad.ManualReplies, contract.ManualReply{
				Comment:    r.Comment,
				Kind:       r.Kind,
				DeferredTo: given(r.DeferredTo),
				BodyFile:   w.add(fmt.Sprintf("manual-reply-%d-body.md", i+1), fmt.Sprintf("done.manual_replies[%d].body", i), r.Body),
			})
		}
		return &contract.Done{Address: ad}, nil
	}
	return nil, fmt.Errorf("stage %q has no draft", in.Stage)
}

// given returns s, or nil when it is absent, empty or blank (work plan D4).
func given(s *string) *string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	v := *s
	return &v
}

// readDraft reads the draft file: a regular file, not a symlink, of at
// most MaxDraft bytes (work plan D3).
func readDraft(path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("no draft path")
	}
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("the agent wrote no draft at %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the draft: %v", err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("the draft at %s is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read the draft: %v", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxDraft+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read the draft: %v", err)
	}
	if len(data) > MaxDraft {
		return nil, fmt.Errorf("the draft is larger than %d bytes", MaxDraft)
	}
	return data, nil
}

// texts collects the text files a result points to, so that every size is
// checked before any file is written (work plan D5).
type texts struct {
	files []textFile
}

type textFile struct {
	name, field, text string
}

// add records a text and returns its file name.
func (t *texts) add(name, field, text string) string {
	t.files = append(t.files, textFile{name, field, text})
	return name
}

// optional records a text that may be absent: an absent, empty or blank
// text gives no file and an empty name (work plan D4).
func (t *texts) optional(name, field string, text *string) string {
	if given(text) == nil {
		return ""
	}
	return t.add(name, field, *text)
}

// write checks every text against contract.MaxTextFile, then writes them
// to dir. An existing file of the same name is replaced, never written
// through.
func (t *texts) write(dir string) error {
	for _, f := range t.files {
		if len(f.text) > contract.MaxTextFile {
			return fmt.Errorf("%s is larger than 64 KiB", f.field)
		}
	}
	for _, f := range t.files {
		path := filepath.Join(dir, f.name)
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("cannot write %s: %v", f.name, err)
		}
		fh, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("cannot write %s: %v", f.name, err)
		}
		_, err = io.WriteString(fh, f.text)
		if cerr := fh.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fmt.Errorf("cannot write %s: %v", f.name, err)
		}
	}
	return nil
}
