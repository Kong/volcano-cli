package output

func commandPathPrefix(values []string) string {
	if len(values) > 0 && values[0] != "" {
		return values[0]
	}
	return "volcano"
}
