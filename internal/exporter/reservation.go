package exporter

import "unicode/utf8"

const maxReservationNameLength = 50

// trimReservationName removes trailing characters from reservation names that exceed the maximum label length.
func trimReservationName(name string) string {
	if name == "" {
		return ""
	}

	if utf8.RuneCountInString(name) <= maxReservationNameLength {
		return name
	}

	runeIndex := 0
	for i := range name {
		if runeIndex == maxReservationNameLength {
			return name[:i]
		}
		runeIndex++
	}

	return name
}
