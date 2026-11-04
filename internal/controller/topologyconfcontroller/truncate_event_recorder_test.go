package topologyconfcontroller

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
)

func Test_TruncateEventRecorder_Eventf_Basic(t *testing.T) {
	base := events.NewFakeRecorder(1)
	recorder := NewTruncateEventRecorder(base)
	long := strings.Repeat("x", 3000)

	recorder.Eventf(&corev1.Pod{}, nil, corev1.EventTypeWarning, "Reason"+long, "Action"+long, "100%% of %s", long)
	recorded := drainEvents(base)
	require.Len(t, recorded, 1)

	parts := strings.SplitN(recorded[0], " ", 3)
	require.Len(t, parts, 3)
	assert.Equal(t, corev1.EventTypeWarning, parts[0])
	assert.Len(t, parts[1], eventLimitReason)
	assert.True(t, strings.HasPrefix(parts[1], "Reason"))
	assert.Len(t, parts[2], eventLimitNote, "the formatted note is bounded, not the template")
	assert.True(t, strings.HasPrefix(parts[2], "100% of xxx"))
	assert.True(t, strings.HasSuffix(parts[2], ellipsis))
}

func Test_truncateString_Basic(t *testing.T) {
	assert.Equal(t, "short", truncateString("short", 10))
	assert.Equal(t, "exactly10!", truncateString("exactly10!", 10))
	assert.Equal(t, "1234567...", truncateString("1234567890x", 10))
	assert.Equal(t, "a...", truncateString("aжжж", 5), "a rune is not cut in half")
}
