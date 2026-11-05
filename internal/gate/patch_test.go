package gate

import (
	"reflect"
	"strings"
	"testing"
)

func TestPatchTargets(t *testing.T) {
	wrap := func(body string) string { return "*** Begin Patch\n" + body + "\n*** End Patch\n" }
	for _, tc := range []struct {
		name, patch string
		want        []string
	}{
		{"empty added file", wrap("*** Add File: empty.txt"), []string{"empty.txt"}},
		{"implicit first chunk", wrap("*** Update File: a.txt\n-old\n+new"), []string{"a.txt"}},
		{"marker whitespace", " \n*** Begin Patch \n  *** Add File: a.txt  \n+x\n *** End Patch\n\n", []string{"a.txt"}},
		{"EOF blank line", wrap("*** Update File: a.txt\n@@\n+x\n*** End of File\n"), []string{"a.txt"}},
		{"chunk after EOF", wrap("*** Update File: a.txt\n@@\n+x\n*** End of File\n@@\n+y"), []string{"a.txt"}},
		{"all operations", wrap("*** Add File: a.txt\n+x\n*** Update File: b.txt\n*** Move to: c.txt\n@@\n-old\n+new\n*** End of File\n*** Delete File: d.txt"), []string{"a.txt", "b.txt", "c.txt", "d.txt"}},
		{"CRLF", strings.ReplaceAll(wrap("*** Delete File: a.txt"), "\n", "\r\n"), []string{"a.txt"}},
		{"header text is content", wrap("*** Update File: a.txt\n@@\n *** Delete File: .naru/config.yaml\n+*** Add File: .naru/fake.json\n-*** Move to: .naru/state"), []string{"a.txt"}},
		{"multiple chunks", wrap("*** Update File: a.txt\n@@ first\n-old\n+new\n@@ second\n context"), []string{"a.txt"}},
		{"path traversal retained", wrap("*** Delete File: src/../.naru/config.yaml"), []string{"src/../.naru/config.yaml"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := patchTargets(tc.patch)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("targets=%v err=%v want=%v", got, err, tc.want)
			}
		})
	}
	for name, patch := range map[string]string{
		"empty":                    "",
		"no operations":            "*** Begin Patch\n*** End Patch",
		"missing footer":           "*** Begin Patch\n*** Delete File: a.txt",
		"trailing operation":       wrap("*** Delete File: a.txt") + "*** Delete File: .naru/config.yaml",
		"missing path":             wrap("*** Add File: "),
		"NUL path":                 wrap("*** Delete File: a\x00.txt"),
		"unknown header":           wrap("*** Rename File: a.txt"),
		"move without update":      wrap("*** Move to: a.txt"),
		"move without diff":        wrap("*** Update File: a.txt\n*** Move to: b.txt"),
		"move after chunk":         wrap("*** Update File: a.txt\n@@\n+x\n*** Move to: b.txt"),
		"double move":              wrap("*** Update File: a.txt\n*** Move to: b.txt\n*** Move to: c.txt\n@@\n+x"),
		"missing update chunk":     wrap("*** Update File: a.txt"),
		"empty update chunk":       wrap("*** Update File: a.txt\n@@"),
		"empty intermediate chunk": wrap("*** Update File: a.txt\n@@\n@@\n+x"),
		"invalid add body":         wrap("*** Add File: a.txt\nunprefixed"),
		"delete body":              wrap("*** Delete File: a.txt\n+x"),
		"body after EOF":           wrap("*** Update File: a.txt\n@@\n+x\n*** End of File\n+y"),
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := patchTargets(patch); err == nil {
				t.Fatalf("accepted malformed patch: %v", got)
			}
		})
	}
}
