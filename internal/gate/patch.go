package gate

import (
	"fmt"
	"strings"
)

// patchTargets reads operation headers, not file contents or diff semantics.
// Unknown or ambiguous syntax must not hide another write from the guard.
func patchTargets(patch string) ([]string, error) {
	patch = strings.ReplaceAll(patch, "\r\n", "\n")
	lines := strings.Split(strings.TrimSpace(patch), "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "*** Begin Patch" || strings.TrimSpace(lines[len(lines)-1]) != "*** End Patch" {
		return nil, fmt.Errorf("expected complete Begin Patch and End Patch markers")
	}
	var targets []string
	operation := ""
	moved, chunk, body, ended := false, false, false, false
	finish := func() error {
		if operation == "update" && (!chunk || !body) {
			return fmt.Errorf("update requires a nonempty diff")
		}
		return nil
	}
	for _, line := range lines[1 : len(lines)-1] {
		marker := strings.TrimRight(line, " \t\r")
		if operation != "update" {
			marker = strings.TrimSpace(line)
		}
		kind, path, header := patchHeader(marker)
		if header {
			if strings.TrimSpace(path) == "" || strings.ContainsAny(path, "\x00\r") {
				return nil, fmt.Errorf("patch operation requires a valid path")
			}
			if kind == "move" {
				if operation != "update" || moved || chunk {
					return nil, fmt.Errorf("move destination must follow an Update File header")
				}
				moved = true
			} else {
				if err := finish(); err != nil {
					return nil, err
				}
				operation = kind
				moved, chunk, body, ended = false, false, false, false
			}
			targets = append(targets, path)
			continue
		}
		switch operation {
		case "add":
			if !strings.HasPrefix(line, "+") {
				return nil, fmt.Errorf("added lines must start with +")
			}
		case "update":
			switch {
			case ended && marker == "":
				continue
			case marker == "@@" || strings.HasPrefix(marker, "@@ "):
				if chunk && !body {
					return nil, fmt.Errorf("empty diff chunk")
				}
				chunk, body, ended = true, false, false
			case ended:
				return nil, fmt.Errorf("unexpected content after End of File")
			case marker == "*** End of File":
				if !chunk || !body {
					return nil, fmt.Errorf("end-of-file marker requires a diff")
				}
				ended = true
			case line == "" || strings.ContainsAny(line[:1], " +-"):
				chunk, body = true, true
			default:
				return nil, fmt.Errorf("invalid update diff line")
			}
		default:
			return nil, fmt.Errorf("expected a file operation header")
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("patch has no file operations")
	}
	if err := finish(); err != nil {
		return nil, err
	}
	return targets, nil
}

func patchHeader(line string) (kind, path string, ok bool) {
	for _, header := range []struct{ prefix, kind string }{
		{"*** Add File: ", "add"},
		{"*** Update File: ", "update"},
		{"*** Delete File: ", "delete"},
		{"*** Move to: ", "move"},
	} {
		if strings.HasPrefix(line, header.prefix) {
			return header.kind, strings.TrimSpace(strings.TrimPrefix(line, header.prefix)), true
		}
	}
	return "", "", false
}
