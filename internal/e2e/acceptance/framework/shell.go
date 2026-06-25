package framework

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
