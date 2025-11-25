package exporter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrimReservationName(t *testing.T) {
	t.Run("returns input when shorter than limit", func(t *testing.T) {
		name := strings.Repeat("b", maxReservationNameLength)
		require.Equal(t, name, trimReservationName(name))
	})

	t.Run("trims when longer than limit", func(t *testing.T) {
		longName := strings.Repeat("a", maxReservationNameLength+10)
		require.Equal(t, longName[:maxReservationNameLength], trimReservationName(longName))
	})

	t.Run("returns empty string when input empty", func(t *testing.T) {
		require.Equal(t, "", trimReservationName(""))
	})
}
