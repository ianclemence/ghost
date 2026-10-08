package main

// conditionWatch retracts an alert once the thing it reported has gone.
//
// An alert ("I'm almost out of storage") is a message in the conversation, so
// it stays after the problem is fixed, still looking like something that needs
// the owner. Each check reports which conditions it could read and whether each
// is present. A condition that stays absent for clearChecks checks in a row is
// resolved. Requiring a run, not one clear reading, keeps a reading that
// hovers at a threshold from flipping an alert off and on.
type conditionWatch struct {
	clear map[string]int
}

// clearChecks is how many consecutive checks a condition must be absent.
const clearChecks = 2

func newConditionWatch() *conditionWatch {
	return &conditionWatch{clear: map[string]int{}}
}

// observe takes one check. present maps each readable condition's alert key to
// whether it is present now; a condition that could not be read is left out and
// is neither counted nor resolved. resolve is called once, on the check that
// completes the run.
func (w *conditionWatch) observe(present map[string]bool, resolve func(key string)) {
	for key, on := range present {
		if on {
			w.clear[key] = 0
			continue
		}
		w.clear[key]++
		if w.clear[key] == clearChecks {
			resolve(key)
		}
	}
}
