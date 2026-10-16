package tuck

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContainerInitializationFailureRetryAndExistingConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "tuck.config.json")
	args := []string{"init", "--container", "--config", path}
	for _, input := range []string{"", "\n", "short\n", strings.Repeat("x", 1025) + "\n"} {
		if e := Run(args, strings.NewReader(input), io.Discard, io.Discard); e == nil {
			t.Fatal("invalid container password accepted")
		}
		if _, e := os.Stat(path); !os.IsNotExist(e) {
			t.Fatal("failed init left a configuration", e)
		}
	}
	if e := Run(args, strings.NewReader(testPassword+"\n"), io.Discard, io.Discard); e != nil {
		t.Fatal(e)
	}
	config, e := LoadConfig(path)
	if e != nil {
		t.Fatal(e)
	}
	c := config.Applied()
	auth := O(O(c["security"])["auth"])
	if S(O(c["server"])["listenHost"]) != "0.0.0.0" || S(O(c["server"])["frontendDir"]) != "/opt/tuck/frontend/dist" || S(O(c["storage"])["dataDir"]) != "/data" || !B(auth["enabled"]) || !VerifyPassword(testPassword, S(auth["passwordHash"])) {
		t.Fatal("incomplete container config")
	}
	before, _ := os.ReadFile(path)
	if e = Run(args, strings.NewReader("different-password\n"), io.Discard, io.Discard); e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("existing configuration overwritten")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal("init temporary file retained", entries)
	}
}
