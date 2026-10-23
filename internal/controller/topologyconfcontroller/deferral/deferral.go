// Package deferral decides when an action that may wait is allowed to run. It combines two
// classic rate controls: a trailing debounce with a maximum wait, which runs the action once what
// it is for has stopped changing for a while but never later than a cap after it first arose, and
// a cooldown, which keeps the action a minimum time away from the previous run.
package deferral

import "time"

// Policy is the timing of a deferred action.
type Policy struct {
	// Cooldown is the least time between the previous run and a deferred one. Zero disables it.
	Cooldown time.Duration
	// Debounce is how long the request has to stay unchanged before it runs.
	Debounce time.Duration
	// DebounceCap is the most the debounce may hold a request that keeps changing, from its Since.
	// Zero disables it.
	DebounceCap time.Duration
}

// FireTime is the earliest moment the request may run, lastRun being the previous run or zero for
// none. The debounce cap bounds the debounce only; it never shortens the cooldown.
func (p Policy) FireTime(lastRun time.Time, r Request) time.Time {
	fireAt := r.LastChange.Add(p.Debounce)
	if p.DebounceCap > 0 {
		if deadline := r.Since.Add(p.DebounceCap); deadline.Before(fireAt) {
			fireAt = deadline
		}
	}
	if !lastRun.IsZero() && p.Cooldown > 0 {
		if cooldownUntil := lastRun.Add(p.Cooldown); cooldownUntil.After(fireAt) {
			fireAt = cooldownUntil
		}
	}
	return fireAt
}

// Request is a deferred action waiting to run.
type Request struct {
	// Since is when the action first became due.
	Since time.Time
	// LastChange is when what the action is for last changed.
	LastChange time.Time
}

// NewRequest is a request that becomes due now.
func NewRequest(now time.Time) Request {
	return Request{Since: now, LastChange: now}
}

// Changed is the request after what it is for changed now.
func (r Request) Changed(now time.Time) Request {
	r.LastChange = now
	return r
}
