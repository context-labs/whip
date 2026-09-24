package main

import (
	"errors"
	"flag"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestMainExitHelper(t *testing.T) {
	if os.Getenv("WHIP_CLI_EXIT_TEST") != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		t.Fatal("helper needs command arguments after --")
	}
	os.Args = append([]string{"whip"}, os.Args[separator+1:]...)
	flag.CommandLine = flag.NewFlagSet("whip", flag.ExitOnError)
	main()
	t.Fatal("invalid command returned without exiting")
}

func TestMainInvalidCommandsHaveStableExitStatusAndExplanation(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"conflicting permissions", []string{"--cautious", "--yolo"}, 2, "mutually exclusive"},
		{"auth usage", []string{"auth"}, 1, "usage:"},
		{"run format", []string{"run", "--format", "xml", "hello"}, 1, "unknown --format"},
		{"daemon usage", []string{"daemon"}, 1, "usage:"},
		{"web usage", []string{"web", "extra"}, 1, "usage:"},
		{"mcp usage", []string{"mcp", "unknown"}, 1, "unknown mcp subcommand"},
		{"skills usage", []string{"skills"}, 1, "usage:"},
		{"acp flag", []string{"acp", "--unknown"}, 1, "flag provided but not defined"},
		{"browser usage", []string{"browser", "unknown"}, 1, "want: install"},
		{"kernel flag", []string{"_kernel", "--unknown"}, 1, "flag provided but not defined"},
		{"daemon flag", []string{"_daemon", "--unknown"}, 1, "flag provided but not defined"},
		{"runtime metadata usage", []string{"_desktop-runtime-info", "extra"}, 1, "could not read runtime build metadata"},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"-test.run=^TestMainExitHelper$", "--"}, test.args...)
			child := exec.CommandContext(t.Context(), self, args...)
			// Keep the Go coverage directory inherited, so command exits are
			// represented in coverage just like successful in-process commands.
			child.Env = append(os.Environ(), "WHIP_CLI_EXIT_TEST=1", "TMPDIR="+t.TempDir())
			output, err := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != test.code {
				t.Fatalf("exit=%v, want %d; output: %s", err, test.code, output)
			}
			if !strings.Contains(string(output), test.want) {
				t.Fatalf("exit %d omitted %q: %s", test.code, test.want, output)
			}
		})
	}
}
