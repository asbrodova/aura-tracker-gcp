package safego

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var goStatement = regexp.MustCompile(`^\s*go\s+\S`)

// TestInternalGoroutinesRecoverPanics keeps new fan-outs on the safe path:
// every goroutine under internal/ must use safego.Group or start with a
// deferred safego.Recover within its first three lines.
func TestInternalGoroutinesRecoverPanics(t *testing.T) {
	root := filepath.Join("..")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if filepath.Base(filepath.Dir(path)) == "safego" {
			return nil
		}
		lines, err := readLines(path)
		if err != nil {
			return err
		}
		for i, line := range lines {
			if strings.Contains(line, `"golang.org/x/sync/errgroup"`) {
				t.Errorf("%s:%d imports errgroup directly; use safego.WithContext", path, i+1)
			}
			if !goStatement.MatchString(line) {
				continue
			}
			window := strings.Join(lines[i+1:min(len(lines), i+4)], "\n")
			if !strings.Contains(window, "safego.Recover(") {
				t.Errorf("%s:%d starts a goroutine without a deferred safego.Recover in its first three lines", path, i+1)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var lines []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}
