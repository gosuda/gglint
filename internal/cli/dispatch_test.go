package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandsAcceptConfigNamesUsedByVettool(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"lint.cfg", "-flags", "-V=full"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(workspace(t, ""), "b")
			writeFile(t, filepath.Join(dir, name), "mode = \"basic\"\n")
			for _, flags := range [][]string{{"-config", name}, {"-config=" + name}} {
				assertResult(t, gglint(t, dir, append([]string{"run"}, flags...)...), result{
					stdout: "gglint: 0 findings in 0 files (mode basic)\n",
					stderr: "gglint: config " + name + "\n",
				})
				assertRules(t,
					listRules(t, dir, append([]string{"-selected"}, flags...)...),
					listRules(t, dir, "-mode", "basic"))
			}
		})
	}
}

func TestCommandsRejectVettoolHandshakeFlags(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"run", "rules"} {
		for _, flag := range []string{"-flags", "-V=full"} {
			t.Run(command+"/"+flag, func(t *testing.T) {
				t.Parallel()
				got := gglint(t, t.TempDir(), command, flag)
				if got.code != 2 || got.stdout != "" || !strings.Contains(got.stderr, "flag provided but not defined") {
					t.Fatalf("%s %s: %+v; want a usage error with empty stdout", command, flag, got)
				}
			})
		}
	}
}
