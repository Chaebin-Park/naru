package policy

import "errors"

// Evaluate inspects current evidence without running an extractor or approving it.
func Evaluate(root string) (report *Report, retErr error) {
	idx, lock, sl, err := policyLocks(root)
	if err != nil {
		return nil, err
	}
	defer func() { retErr = errors.Join(retErr, sl.Close(), lock.Close()) }()
	_, _, report, err = inspect(lock.Root, idx)
	return report, err
}
