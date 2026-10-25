// Package cli supplies small standalone-CLI conventions, without application,
// provider, model or job dependencies.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }
func Errorf(code int, format string, args ...any) error {
	return &ExitError{code, fmt.Errorf(format, args...)}
}
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var e *ExitError
	if errors.As(err, &e) {
		return e.Code
	}
	return 1
}

func WriteJSON(w io.Writer, value any) error {
	e := json.NewEncoder(w)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	return e.Encode(value)
}

// OrderFlags preserves repeated-flag order and allows flags after positional
// arguments, using the FlagSet as the only parameter schema.
func OrderFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var options, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		name, _, assigned := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		f := fs.Lookup(name)
		if f == nil {
			return nil, fmt.Errorf("unknown option %q", arg)
		}
		options = append(options, arg)
		isBool := false
		if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok {
			isBool = b.IsBoolFlag()
		}
		if !assigned && !isBool {
			if i+1 == len(args) {
				return nil, fmt.Errorf("option --%s requires a value", name)
			}
			i++
			options = append(options, args[i])
		}
	}
	return append(append(options, "--"), positional...), nil
}

// PrintFlags derives help from actual parser definitions, including defaults.
func PrintFlags(w io.Writer, fs *flag.FlagSet) {
	fs.VisitAll(func(f *flag.Flag) {
		name, description := flag.UnquoteUsage(f)
		label := "--" + f.Name
		if len(f.Name) == 1 {
			label = "-" + f.Name
		}
		if name != "" {
			label += " " + name
		}
		fmt.Fprintf(w, "  %s\n      %s", label, description)
		if f.DefValue != "" {
			fmt.Fprintf(w, " (default: %s)", f.DefValue)
		}
		fmt.Fprintln(w)
	})
}
