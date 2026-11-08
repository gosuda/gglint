package consts

func thrice() []string {
	return []string{first(), second(), third()}
}

func first() string {
	return "alpha" // want `string "alpha" has 3 occurrences, make it a constant`
}

func second() string {
	return "alpha"
}

func third() string {
	return "alpha"
}

func twice() string {
	if pair() == "" {
		return "beta"
	}
	return "beta"
}

func pair() string {
	return ""
}

func shortest() string {
	if pair() == "" {
		return "abc" // want `string "abc" has 3 occurrences, make it a constant`
	}
	if second() == "" {
		return "abc"
	}
	return "abc"
}

func tooShort() string {
	if pair() == "" {
		return "ab"
	}
	if second() == "" {
		return "ab"
	}
	return "ab"
}
