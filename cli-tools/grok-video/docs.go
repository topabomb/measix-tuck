package main

import (
	_ "embed"
	"io"
)

//go:embed README.md
var manual string

const docsHelp = `Usage: grok-video docs

Print the embedded README.md verbatim as UTF-8 Markdown on stdout:
  usage, supported models/capabilities, protocol research, every command,
  every registered option/default, examples and exit codes.

README.md is the single complete manual source, embedded at compile time.
Output is byte-for-byte identical: no generated reference or added version.
No credentials, source directory, extra files or network are needed.
There are no options. Use shell redirection to save the string:
  grok-video docs > grok-video-manual.md

Exit: 0 success, 1 output error.
`

func writeDocs(w io.Writer) error {
	_, err := io.WriteString(w, manual)
	return err
}
