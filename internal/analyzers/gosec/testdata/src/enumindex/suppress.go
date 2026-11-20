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

// A tag mid-sentence is prose, not a directive.
func ProseNoSecReports(t token) string {
	names := [...]string{"light", "dark"}
	if tokenCount < t {
		return "unknown"
	}
	// callers may write #nosec G602 in prose; it suppresses nothing
	return names[t] // want "G602: slice index out of range"
}

// A directive with prose but no rule IDs suppresses every rule.
func RulelessSuppressed(t token) string {
	names := [...]string{"light", "dark"}
	if tokenCount < t {
		return "unknown"
	}
	// #nosec validated by the caller
	return names[t]
}

// The first recognized directive on a statement is the only one applied.
func FirstDirectiveOnly(t token) string {
	names := [...]string{"light", "dark"}
	if tokenCount < t {
		return "unknown"
	}
	// #nosec G401
	return names[t] // #nosec G602 // want "G602: slice index out of range"
}

// A trailing directive covers the whole attached statement.
func TrailingDirectiveCoversCall(t token) string {
	names := [...]string{"light", "dark"}
	if tokenCount < t {
		return "unknown"
	}
	println(
		names[t]) // #nosec G602
	return "ok"
}
