package enumindex

// Directives suppress only the access whose statement they cover.
func SuppressedAccess(t token) string {
	names := [...]string{"light", "dark"}
	if tokenCount < t {
		return "unknown"
	}
	// #nosec G602 -- caller validates the token
	return names[t]
}

func LineSuppressedAccess(t token) string {
	names := [...]string{"light", "dark"}
	if tokenCount < t {
		return "unknown"
	}
	// #nosec G602 -- covers the statement below
	_ = names[t]
	return names[t] // want "G602: slice index out of range"
}

func OtherRuleStillReports(t token) string {
	names := [...]string{"light", "dark"}
	if tokenCount < t {
		return "unknown"
	}
	// #nosec G401 -- a different rule does not suppress G602
	return names[t] // want "G602: slice index out of range"
}

func NakedNoSecReports(t token) string {
	names := [...]string{"light", "dark"}
	if tokenCount < t {
		return "unknown"
	}
	// #nosec
	return names[t]
}

func DirectiveDisabled(t token) string {
	names := [...]string{"light", "dark"}
	if tokenCount < t {
		return "unknown"
	}
	//gosec:disable G602
	return names[t]
}
