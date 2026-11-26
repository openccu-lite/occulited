package radio

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// B-188: Go keeps the sticky, setgid and setuid bits in os.ModeSticky, os.ModeSetgid and
// os.ModeSetuid; an os.FileMode literal such as 0o1775 silently drops them (it is 0775 with a stray
// bit Go ignores). No such literal in occulited's code: the special bits are written as the
// os.Mode* constants. A raw syscall mode (unix.Chmod) would be the one place for one, and there is
// none. 0o7777 is the mask of a raw stat mode (uint32, not an os.FileMode) and is not a mode.
func TestNoSpecialBitsAsOctal(t *testing.T) {
	re := regexp.MustCompile(`\b0o?[1-7][0-7]{3}\b`)
	root, _ := filepath.Abs("../..")
	var hits []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == "ui" || d.Name() == ".git" || d.Name() == "dist") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		for i, line := range strings.Split(string(b), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if strings.Contains(code, "\"") || strings.Contains(code, "`") {
				continue // a string, not a mode
			}
			if re.MatchString(strings.ReplaceAll(code, "0o7777", "")) {
				hits = append(hits, strings.TrimPrefix(p, root+"/")+":"+itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if len(hits) > 0 {
		t.Errorf("an octal mode with a special bit (write os.ModeSticky/ModeSetgid/ModeSetuid):\n%s", strings.Join(hits, "\n"))
	}
}
