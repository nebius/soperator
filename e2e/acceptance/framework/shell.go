package framework

import "strings"

func ShellQuote(value string) string {
	result := "'"
	for _, r := range value {
		if r == '\'' {
			result += `'"'"'`
			continue
		}
		result += string(r)
	}
	result += "'"
	return result
}

// ShellJoin quotes every word and joins them into one command line, so that a
// shell parses it back into exactly the given words.
func ShellJoin(words ...string) string {
	quoted := make([]string, 0, len(words))
	for _, word := range words {
		quoted = append(quoted, ShellQuote(word))
	}
	return strings.Join(quoted, " ")
}

func BashLC(script string) string {
	return "bash -lc " + ShellQuote(script)
}
