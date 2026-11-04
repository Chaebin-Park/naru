//go:build !unix

package receipt

// Directory durability is not established on non-Unix platforms. File sync and
// rename errors still propagate; physical power-loss recovery is not promised.
func syncDirectory(string) error { return nil }
