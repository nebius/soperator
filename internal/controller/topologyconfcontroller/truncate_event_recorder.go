package topologyconfcontroller

import (
	"fmt"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
)

const (
	eventLimitNote   = 1024
	eventLimitAction = 128
	eventLimitReason = 128

	ellipsis = "..."
)

type TruncateEventRecorder struct {
	baseRecorder events.EventRecorder
}

func NewTruncateEventRecorder(baseRecorder events.EventRecorder) *TruncateEventRecorder {
	return &TruncateEventRecorder{
		baseRecorder: baseRecorder,
	}
}

func (r *TruncateEventRecorder) Eventf(regarding runtime.Object, related runtime.Object, eventtype, reason, action, note string, args ...interface{}) {
	// Format the note before truncating it.
	note = truncateString(fmt.Sprintf(note, args...), eventLimitNote)
	action = truncateString(action, eventLimitAction)
	reason = truncateString(reason, eventLimitReason)

	r.baseRecorder.Eventf(regarding, related, eventtype, reason, action, "%s", note)
}

func truncateString(str string, limitBytes int) string {
	if len(str) <= limitBytes {
		return str
	}
	cut := limitBytes - len(ellipsis)
	for cut > 0 && !utf8.RuneStart(str[cut]) {
		cut--
	}
	return str[:cut] + ellipsis
}
