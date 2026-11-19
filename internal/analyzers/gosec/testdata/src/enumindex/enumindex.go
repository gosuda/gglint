package enumindex

type token uint8

const (
	tokenLight token = iota
	tokenDark
	tokenCount
)

func (t token) String() string {
	names := [...]string{"light", "dark"}
	if t >= tokenCount {
		return "unknown"
	}
	return names[t]
}

func OffByOne(t token) string {
	names := [...]string{"light", "dark"}
	if t > tokenCount {
		return "unknown"
	}
	return names[t] // want "G602: slice index out of range"
}

func ConstantOnLeft(t token) string {
	names := [...]string{"light", "dark"}
	if tokenCount <= t {
		return "unknown"
	}
	return names[t]
}

func ConstOnLeftInsufficient(t token) string {
	names := [...]string{"light", "dark"}
	if tokenCount < t {
		return "unknown"
	}
	return names[t] // want "G602: slice index out of range"
}

func EqualityGuarded(t token) string {
	names := [...]string{"light", "dark"}
	if tokenCount < t {
		return "unknown"
	}
	if t == tokenCount {
		return "unknown"
	}
	return names[t]
}

func NotEqualTrueBranch(t token) string {
	names := [...]string{"light", "dark"}
	if t != tokenDark {
		return names[t] // want "G602: slice index out of range"
	}
	return "unknown"
}

func CapturedIndex(t token) string {
	names := [...]string{"light", "dark"}
	defer func() { _ = t }()
	if tokenCount < t {
		return "unknown"
	}
	return names[t]
}

func TrueBranch(t token) string {
	names := [...]string{"light", "dark"}
	if t < tokenCount {
		return names[t]
	}
	return "unknown"
}

func InclusiveGuard(t token) string {
	names := [...]string{"light", "dark"}
	if t > tokenDark {
		return "unknown"
	}
	return names[t]
}

func RejoiningGuard(t token) string {
	names := [...]string{"light", "dark"}
	prefix := "valid: "
	if t >= tokenCount {
		prefix = "invalid: "
	}
	return prefix + names[t] // want "G602: slice index out of range"
}

func GuardAfterAccess(t token) string {
	names := [...]string{"light", "dark"}
	name := names[t] // want "G602: slice index out of range"
	if t >= tokenCount {
		return "unknown"
	}
	return name
}

func GuardOtherIndex(t, other token) string {
	names := [...]string{"light", "dark"}
	if other >= tokenCount {
		return "unknown"
	}
	if t > tokenCount {
		return "unknown"
	}
	return names[t] // want "G602: slice index out of range"
}

func ChangedIndex(t token) string {
	names := [...]string{"light", "dark"}
	if t >= tokenCount {
		return "unknown"
	}
	t += tokenCount
	return names[t] // want "G602: slice index out of range"
}

type signedToken int8

func SignedUpperBound(t signedToken) string {
	names := [...]string{"light", "dark"}
	if t >= signedToken(tokenCount) {
		return "unknown"
	}
	return names[t] // want "G602: slice index out of range"
}

func ShortenedSlice(t token) string {
	names := []string{"light", "dark"}
	names = names[:1]
	if t >= tokenCount {
		return "unknown"
	}
	return names[t] // want "G602: slice index out of range"
}

func LoopBeforeGuard(t token, again bool) string {
	names := [...]string{"light", "dark"}
	for {
		name := names[t] // want "G602: slice index out of range"
		if t >= tokenCount {
			return "unknown"
		}
		if !again {
			return name
		}
	}
}

func SignedToUnsigned(t signedToken) string {
	names := [...]string{"light", "dark"}
	if t >= signedToken(tokenCount) {
		return "unknown"
	}
	index := token(t)    // want "G115: integer overflow conversion int8 -> uint8"
	name := names[index] // want "G602: slice index out of range"
	if index >= tokenCount {
		return "unknown"
	}
	return name
}
