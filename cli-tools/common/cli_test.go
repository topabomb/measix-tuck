package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestOrderFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var n int
	var ok bool
	var text string
	fs.IntVar(&n, "count", 1, "count")
	fs.BoolVar(&ok, "wait", false, "wait")
	fs.StringVar(&text, "text", "", "text")
	args, err := OrderFlags(fs, []string{"id", "--count", "-2", "--wait", "--text=hello", "--", "--literal"})
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	if n != -2 || !ok || text != "hello" || strings.Join(fs.Args(), ",") != "id,--literal" {
		t.Fatal(n, ok, text, fs.Args())
	}
	for _, args := range [][]string{{"--bad"}, {"--count"}} {
		if _, err := OrderFlags(fs, args); err == nil {
			t.Fatal(args)
		}
	}
}

func TestErrorsJSONAndHelp(t *testing.T) {
	base := errors.New("source")
	err := fmt.Errorf("wrapped: %w", &ExitError{Code: 4, Err: base})
	if ExitCode(err) != 4 || !errors.Is(err, base) || ExitCode(nil) != 0 || ExitCode(base) != 1 || Errorf(2, "bad %s", "input").Error() != "bad input" {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := WriteJSON(&out, map[string]any{"html": "<tag>"}); err != nil || !strings.Contains(out.String(), "<tag>") {
		t.Fatal(out.String(), err)
	}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("name", "sample", "Use `NAME`.")
	fs.Bool("h", false, "Help.")
	out.Reset()
	PrintFlags(&out, fs)
	for _, s := range []string{"--name NAME", "default: sample", "-h"} {
		if !strings.Contains(out.String(), s) {
			t.Fatal(out.String())
		}
	}
}
