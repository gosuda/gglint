package checkescape

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// ListExport returns an Archive that asks the go command, with the given
// build tags, for the compiled archive of the package under analysis. The
// returned path is a build-cache file owned by the go command.
func ListExport(tags []string) Archive {
	tagsFlag := "-tags=" + strings.Join(tags, ",")
	return func(pass *analysis.Pass) (string, error) {
		dir := filepath.Dir(pass.Fset.Position(pass.Files[0].Package).Filename)
		return listExport(dir, tagsFlag)
	}
}

func listExport(dir, tagsFlag string) (string, error) {
	var stderr bytes.Buffer
	cmd := exec.Command("go", "list", "-f", "{{.Export}}", "-export", tagsFlag, ".")
	cmd.Dir = dir
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go list -export in %s: %w: %s", dir, err, strings.TrimSpace(stderr.String()))
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", fmt.Errorf("go list -export printed no archive for %s", dir)
	}
	return path, nil
}
