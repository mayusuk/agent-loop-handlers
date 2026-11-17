// Command fakeclaude stands in for the claude CLI in tests, so no test
// calls the real Claude Code or the Anthropic API. Tests build it with
// fakebin.Build in TestMain and script it with environment variables. It
// does each step in this order, skipping the ones whose variable is unset:
//
//	FAKECLAUDE_RECORD         write a Record of this call (JSON) to this path
//	FAKECLAUDE_CHILD_PIDFILE  start a child "sleep 600" and write its PID here
//	FAKECLAUDE_IGNORE_TERM    "1": ignore SIGTERM
//	FAKECLAUDE_SHELL          run this with "sh -c" in the working directory
//	FAKECLAUDE_DRAFT          write FAKECLAUDE_DRAFT_CONTENT to this path
//	FAKECLAUDE_STDERR         print this to stderr
//	FAKECLAUDE_SLEEP          sleep for this Go duration
//	FAKECLAUDE_OUTPUT         print this to stdout instead of DefaultOutput
//	FAKECLAUDE_EXIT           exit with this code (default 0)
//
// A failing step prints "fakeclaude: ..." to stderr and exits 99.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

// DefaultOutput is what the fake prints when FAKECLAUDE_OUTPUT is unset: a
// result in the shape of claude --output-format json.
const DefaultOutput = `{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"fake",` +
	`"total_cost_usd":0.25,"usage":{"input_tokens":100,"cache_creation_input_tokens":20,"cache_read_input_tokens":3,"output_tokens":40}}`

// Record is what FAKECLAUDE_RECORD receives.
type Record struct {
	Args []string `json:"args"`
	Dir  string   `json:"dir"`
	// StdinEOF is true when a read from stdin returned end of file at once.
	StdinEOF bool `json:"stdin_eof"`
	// Pgid is the fake's process group.
	Pgid int `json:"pgid"`
	Pid  int `json:"pid"`
}

func main() {
	if p := os.Getenv("FAKECLAUDE_RECORD"); p != "" {
		dir, err := os.Getwd()
		check(err)
		pgid, err := syscall.Getpgid(0)
		check(err)
		rec := Record{Args: os.Args[1:], Dir: dir, StdinEOF: stdinEOF(), Pgid: pgid, Pid: os.Getpid()}
		data, err := json.Marshal(rec)
		check(err)
		check(os.WriteFile(p, data, 0o644))
	}
	if p := os.Getenv("FAKECLAUDE_CHILD_PIDFILE"); p != "" {
		child := exec.Command("sleep", "600")
		check(child.Start())
		check(os.WriteFile(p, []byte(strconv.Itoa(child.Process.Pid)), 0o644))
	}
	if os.Getenv("FAKECLAUDE_IGNORE_TERM") == "1" {
		signal.Ignore(syscall.SIGTERM)
	}
	if s := os.Getenv("FAKECLAUDE_SHELL"); s != "" {
		cmd := exec.Command("sh", "-c", s)
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		check(cmd.Run())
	}
	if p := os.Getenv("FAKECLAUDE_DRAFT"); p != "" {
		check(os.WriteFile(p, []byte(os.Getenv("FAKECLAUDE_DRAFT_CONTENT")), 0o644))
	}
	if s := os.Getenv("FAKECLAUDE_STDERR"); s != "" {
		fmt.Fprintln(os.Stderr, s)
	}
	if s := os.Getenv("FAKECLAUDE_SLEEP"); s != "" {
		d, err := time.ParseDuration(s)
		check(err)
		time.Sleep(d)
	}
	out, ok := os.LookupEnv("FAKECLAUDE_OUTPUT")
	if !ok {
		out = DefaultOutput
	}
	fmt.Fprint(os.Stdout, out)
	if s := os.Getenv("FAKECLAUDE_EXIT"); s != "" {
		code, err := strconv.Atoi(s)
		check(err)
		os.Exit(code)
	}
}

// stdinEOF reports whether stdin is at end of file. A read on an open
// terminal or pipe with no writer closing it would block, so the read runs
// with a short timeout.
func stdinEOF() bool {
	done := make(chan bool, 1)
	go func() {
		n, err := os.Stdin.Read(make([]byte, 1))
		done <- n == 0 && err == io.EOF
	}()
	select {
	case eof := <-done:
		return eof
	case <-time.After(2 * time.Second):
		return false
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "fakeclaude: %v\n", err)
		os.Exit(99)
	}
}
