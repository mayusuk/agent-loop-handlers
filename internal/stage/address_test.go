package stage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mayusuk/agent-loop/contract"
)

const (
	repliesAll = `{"result":"done","done":{` +
		`"replies":[{"finding":"F1","kind":"fixed","body":"Fixed."},{"finding":"F2","kind":"deferred","deferred_to":"9","body":"Later."}],` +
		`"manual_replies":[{"comment":"MC-1","kind":"explained","body":"Because."}]}}`
	repliesNoF2 = `{"result":"done","done":{` +
		`"replies":[{"finding":"F1","kind":"fixed","body":"Fixed."}],` +
		`"manual_replies":[{"comment":"MC-1","kind":"explained","body":"Because."}]}}`
	repliesNoMC1 = `{"result":"done","done":{` +
		`"replies":[{"finding":"F1","kind":"fixed","body":"Fixed."},{"finding":"F2","kind":"not_valid","body":"No."}]}}`
	noReplies = `{"result":"done","done":{"replies":[],"manual_replies":[]}}`
)

// addressInput is the address input for the fixture, answering F1, F2
// and MC-1 in findings mode.
func (f *fixture) addressInput(mode contract.AddressMode) contract.Input {
	in := f.input(contract.StageAddress)
	in.Change = &contract.ChangeRef{Ref: "2", URL: "https://github.com/o/r/pull/2", Head: f.head, BaseTip: f.baseTip}
	in.Address = &contract.AddressInput{Mode: mode, AnswerFindings: []string{"F1", "F2"}, AnswerManual: []string{"MC-1"}}
	if mode == contract.ModeBaseUpdate {
		in.Address.AnswerFindings, in.Address.AnswerManual = []string{}, []string{}
	}
	return in
}

func (f *fixture) address(in contract.Input, shell, draft string, extra ...string) outcome {
	f.t.Helper()
	return f.runStage(in, Address, shell, draft, extra...)
}

func addressDone(t *testing.T, o outcome) *contract.AddressDone {
	t.Helper()
	if o.res.Result != contract.OutcomeDone {
		t.Fatalf("result = %s: %+v\n%s", o.res.Result, o.res.Failed, o.stderr)
	}
	return o.res.Done.Address
}

// unpushed fails the test if the remote branch moved from before.
func (f *fixture) unpushed(before string) {
	f.t.Helper()
	if h := f.remoteHead(); h != before {
		f.t.Errorf("remote head = %s, want %s: nothing may be pushed", h, before)
	}
}

func TestFR5AC2_RepliesWithNoPushKeepTheHead(t *testing.T) {
	for _, mode := range []contract.AddressMode{contract.ModeFindings, contract.ModeManual} {
		t.Run(string(mode), func(t *testing.T) {
			f := newFixture(t, true)
			o := f.address(f.addressInput(mode), draftOnly, repliesAll)
			d := addressDone(t, o)
			if d.Head != f.head {
				t.Errorf("head = %s, want the input head %s", d.Head, f.head)
			}
			f.unpushed(f.head)
			if len(d.Replies) != 2 || len(d.ManualReplies) != 1 {
				t.Fatalf("replies %+v manual %+v", d.Replies, d.ManualReplies)
			}
			if d.Replies[1].DeferredTo == nil || *d.Replies[1].DeferredTo != "9" {
				t.Errorf("deferred_to = %v", d.Replies[1].DeferredTo)
			}
			if b, _ := os.ReadFile(filepath.Join(o.out, d.ManualReplies[0].BodyFile)); string(b) != "Because." {
				t.Errorf("manual reply body = %q", b)
			}
			if o.res.Usage == nil || o.res.Usage.Cost == nil {
				t.Errorf("usage = %+v, want the fake's", o.res.Usage)
			}
		})
	}
}

func TestFR5AC2_FixCommitIsPushedAsHead(t *testing.T) {
	f := newFixture(t, true)
	o := f.address(f.addressInput(contract.ModeFindings), commitAndDraft, repliesAll)
	d := addressDone(t, o)
	if d.Head == f.head || d.Head != f.remoteHead() {
		t.Fatalf("head = %s, remote head = %s, input head %s", d.Head, f.remoteHead(), f.head)
	}
	if p := git(t, f.ws, "rev-parse", d.Head+"^"); p != f.head {
		t.Errorf("fix commit's parent = %s, want %s", p, f.head)
	}
}

func TestFR5AC2_MissingReplyWithUnmovedHeadIsFailed(t *testing.T) {
	f := newFixture(t, true)
	o := f.address(f.addressInput(contract.ModeFindings), draftOnly, repliesNoF2)
	if r := failedReason(t, o); r != "the head did not move and F2 have no reply (FR-5 AC2)" {
		t.Errorf("reason = %q", r)
	}
	f.unpushed(f.head)
}

func TestFR5AC2_MissingFindingReplyWithMovedHeadIsDone(t *testing.T) {
	f := newFixture(t, true)
	d := addressDone(t, f.address(f.addressInput(contract.ModeFindings), commitAndDraft, repliesNoF2))
	if d.Head != f.remoteHead() || d.Head == f.head {
		t.Errorf("head = %s, remote %s", d.Head, f.remoteHead())
	}
}

func TestD1_MissingManualReplyIsFailedAndNotPushed(t *testing.T) {
	f := newFixture(t, true)
	o := f.address(f.addressInput(contract.ModeFindings), commitAndDraft, repliesNoMC1)
	if r := failedReason(t, o); r != "manual comments MC-1 have no reply (FR-5 AC2); nothing was pushed" {
		t.Errorf("reason = %q", r)
	}
	f.unpushed(f.head)
}

func TestFR18AC1_BlockedAndNeedsOwnerPushNothing(t *testing.T) {
	for name, draft := range map[string]string{"blocked": blockedDraft, "needs_owner": needsOwnerDraft} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, true)
			o := f.address(f.addressInput(contract.ModeFindings), commitAndDraft, draft)
			if string(o.res.Result) != name {
				t.Fatalf("result = %s, want %s\n%s", o.res.Result, name, o.stderr)
			}
			f.unpushed(f.head)
		})
	}
}

func TestD4_AddressHeadNotBuildingOnTheInputIsFailed(t *testing.T) {
	f := newFixture(t, true)
	o := f.address(f.addressInput(contract.ModeFindings), `git reset -q --hard origin/main && `+commitAndDraft, repliesAll)
	if r := failedReason(t, o); !strings.Contains(r, "does not build on "+f.head) {
		t.Errorf("reason = %q", r)
	}
	f.unpushed(f.head)
}

func TestD2_AddressWithoutAddressObjectIsFailed(t *testing.T) {
	f := newFixture(t, true)
	in := f.addressInput(contract.ModeFindings)
	in.Address = nil
	if r := failedReason(t, f.address(in, `touch ran && `+draftOnly, repliesAll)); r != "the input has no address object" {
		t.Errorf("reason = %q", r)
	}
	if _, err := os.Stat(filepath.Join(f.ws, "ran")); err == nil {
		t.Errorf("claude ran")
	}
}

// newBaseFixture makes agent/1 and main both change f.txt, so merging
// main into agent/1 conflicts, and a workspace on agent/1 (§5.3 step 3).
func newBaseFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{t: t, root: root, remote: filepath.Join(root, "remote.git"), ws: filepath.Join(root, "ws")}
	git(t, root, "init", "-q", "--bare", "-b", "main", f.remote)
	seed := filepath.Join(root, "seed")
	git(t, root, "clone", "-q", f.remote, seed)
	f.identity(seed)
	write := func(text, msg string) {
		os.WriteFile(filepath.Join(seed, "f.txt"), []byte(text), 0o644)
		git(t, seed, "add", "f.txt")
		git(t, seed, "commit", "-q", "-m", msg)
	}
	write("base\n", "base")
	git(t, seed, "push", "-q", "origin", "HEAD:main")
	git(t, seed, "checkout", "-q", "-b", "agent/1")
	write("ours\n", "ours")
	git(t, seed, "push", "-q", "origin", "agent/1")
	git(t, seed, "checkout", "-q", "main")
	write("theirs\n", "theirs")
	git(t, seed, "push", "-q", "origin", "main")
	f.baseTip = git(t, seed, "rev-parse", "HEAD")
	git(t, root, "clone", "-q", f.remote, f.ws)
	f.identity(f.ws)
	git(t, f.ws, "checkout", "-q", "agent/1")
	f.head = git(t, f.ws, "rev-parse", "HEAD")
	return f
}

const (
	// mergeBase is the fake agent merging the base and resolving f.txt.
	mergeBase = `git merge -q origin/main >/dev/null 2>&1; echo both > f.txt && git add f.txt && git commit -q --no-edit`
)

func TestFR5AC6_BaseMergeIsPushedAndReturned(t *testing.T) {
	f := newBaseFixture(t)
	o := f.address(f.addressInput(contract.ModeBaseUpdate), mergeBase+` && `+draftOnly, noReplies)
	d := addressDone(t, o)
	if d.Head != f.remoteHead() || d.Head == f.head {
		t.Fatalf("head = %s, remote %s, input %s", d.Head, f.remoteHead(), f.head)
	}
	if p := git(t, f.ws, "rev-list", "--parents", "-n", "1", d.Head); p != d.Head+" "+f.head+" "+f.baseTip {
		t.Errorf("parents = %q, want the input head then the base tip", p)
	}
	if len(d.Replies) != 0 || len(d.ManualReplies) != 0 {
		t.Errorf("replies %+v %+v", d.Replies, d.ManualReplies)
	}
}

func TestFR5AC6_WrongShapeIsFailedAndNotPushed(t *testing.T) {
	cases := map[string]struct{ shell, want string }{
		"rebase": {
			`git reset -q --hard origin/main && echo both > f.txt && git commit -qam rebased`,
			"is not a merge commit of two parents (it has 1)",
		},
		"one parent": {
			`echo both > f.txt && git commit -qam fix`,
			"is not a merge commit of two parents (it has 1)",
		},
		"merge then a commit": {
			mergeBase + ` && echo more > g.txt && git add g.txt && git commit -qm more`,
			"is not a merge commit of two parents (it has 1)",
		},
		"a commit then the merge": {
			`echo more > g.txt && git add g.txt && git commit -qm more && ` + mergeBase,
			"is not one merge commit on " + "%HEAD%",
		},
		"no commit": {
			`true`,
			"no merge commit since %HEAD%",
		},
		"merge of an older base": {
			"git reset -q --hard $(git commit-tree -p HEAD -p origin/main~1 -m older 'HEAD^{tree}')",
			"does not contain base tip",
		},
		"merge of a side branch": {
			`git checkout -qb side origin/main && echo s > s.txt && git add s.txt && git commit -qm s && git checkout -q agent/1 && ` +
				`git merge -q side >/dev/null 2>&1; echo both > f.txt && git add f.txt && git commit -q --no-edit`,
			"is not on origin/main",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newBaseFixture(t)
			o := f.address(f.addressInput(contract.ModeBaseUpdate), c.shell+` && `+draftOnly, noReplies)
			r := failedReason(t, o)
			if want := strings.ReplaceAll(c.want, "%HEAD%", f.head); !strings.Contains(r, want) || !strings.HasSuffix(r, "; nothing was pushed") {
				t.Errorf("reason = %q, want it to contain %q", r, want)
			}
			f.unpushed(f.head)
		})
	}
}

func TestFR5AC6_BaseUpdateRepliesAreFailed(t *testing.T) {
	f := newBaseFixture(t)
	o := f.address(f.addressInput(contract.ModeBaseUpdate), mergeBase+` && `+draftOnly, repliesAll)
	if r := failedReason(t, o); r != "a base_update address run must not reply (FR-5 AC6); nothing was pushed" {
		t.Errorf("reason = %q", r)
	}
	f.unpushed(f.head)
}

func TestD2_BaseUpdateWithoutBaseTipIsFailed(t *testing.T) {
	f := newBaseFixture(t)
	in := f.addressInput(contract.ModeBaseUpdate)
	in.Change = nil
	if r := failedReason(t, f.address(in, `touch ran && `+draftOnly, noReplies)); r != "a base_update address run needs change.base_tip" {
		t.Errorf("reason = %q", r)
	}
	if _, err := os.Stat(filepath.Join(f.ws, "ran")); err == nil {
		t.Errorf("claude ran")
	}
}
