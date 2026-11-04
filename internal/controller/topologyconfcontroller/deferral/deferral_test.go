package deferral

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var t0 = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func at(d time.Duration) time.Time { return t0.Add(d) }

func Test_NewRequest_Basic(t *testing.T) {
	assert.Equal(t, Request{Since: t0, LastChange: t0}, NewRequest(t0))
}

func Test_Request_Changed_Basic(t *testing.T) {
	r := NewRequest(t0).Changed(at(time.Minute))
	assert.Equal(t, Request{Since: t0, LastChange: at(time.Minute)}, r)
}

func Test_Policy_FireTime_Basic(t *testing.T) {
	policy := Policy{Cooldown: 15 * time.Minute, Debounce: time.Minute, DebounceCap: 5 * time.Minute}
	tests := []struct {
		name    string
		lastRun time.Time
		request Request
		want    time.Time
	}{
		{name: "no previous run: debounce", request: NewRequest(t0), want: at(time.Minute)},
		{name: "old previous run: debounce", lastRun: at(-time.Hour), request: NewRequest(t0), want: at(time.Minute)},
		{name: "a change restarts the debounce", lastRun: at(-time.Hour), request: NewRequest(t0).Changed(at(2 * time.Minute)), want: at(3 * time.Minute)},
		{name: "the cap bounds the debounce", lastRun: at(-time.Hour), request: NewRequest(t0).Changed(at(5 * time.Minute)), want: at(5 * time.Minute)},
		{name: "the cooldown outlasts the debounce", lastRun: at(-time.Minute), request: NewRequest(t0), want: at(14 * time.Minute)},
		{name: "the cooldown outlasts the cap", lastRun: at(-time.Minute), request: NewRequest(t0).Changed(at(5 * time.Minute)), want: at(14 * time.Minute)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, policy.FireTime(tt.lastRun, tt.request))
		})
	}
}

func Test_Policy_FireTime_ZeroPolicy(t *testing.T) {
	assert.Equal(t, t0, Policy{}.FireTime(at(-time.Second), NewRequest(t0)), "no cooldown, no debounce: runs at once")
	assert.Equal(t, at(time.Minute), Policy{Debounce: time.Minute}.FireTime(at(-time.Second), NewRequest(t0)), "a zero cooldown is disabled")
}
