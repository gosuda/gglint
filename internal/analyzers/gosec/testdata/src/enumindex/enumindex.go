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

func CompoundBoundary(t token) string {
	names := [...]string{"light", "dark"}
	if t == tokenLight || t == tokenCount {
		return names[t] // want "G602: slice index out of range"
	}
	return "unknown"
}

var lightLabel = "light"

func GlobalElement(t token) string {
	names := [...]string{lightLabel, "dark"}
	if tokenCount < t {
		return "unknown"
	}
	return names[t] // want "G602: slice index out of range"
}

type namedNames [2]string

func NamedArray(t token) string {
	var names namedNames = [...]string{"light", "dark"}
	if tokenCount < t {
		return "unknown"
	}
	return names[t] // want "G602: slice index out of range"
}

func SlicedAfterAccess(t token) (string, []string) {
	names := [...]string{"light", "dark"}
	if tokenCount < t {
		return "unknown", nil
	}
	name := names[t] // want "G602: slice index out of range"
	return name, names[:]
}

func tokenLabel(t token) string { return "" }

func ReturnCallOperand(t token) (string, string) {
	names := [...]string{"light", "dark"}
	if tokenCount < t {
		return "unknown", ""
	}
	return names[t], tokenLabel(t)
}

func NewNamedArray(t token) string {
	new := [...]string{"light", "dark"}
	if tokenCount < t {
		return "unknown"
	}
	return new[t] // want "G602: slice index out of range"
}

func ParamArray(new [2]string, t token) string {
	if tokenCount < t {
		return "unknown"
	}
	return new[t]
}

func PointerArray(t token) string {
	p := &[2]string{"light", "dark"}
	if tokenCount < t {
		return "unknown"
	}
	return p[t]
}

func ExplicitDeref(t token, p *token) string {
	names := [...]string{"light", "dark"}
	if *p == tokenLight {
		return "unknown"
	}
	if tokenCount < t {
		return "unknown"
	}
	return names[t]
}

func validate(t token) {}

func ValidatedBoundary(t token) string {
	names := [...]string{"light", "dark"}
	if t == tokenLight || t == tokenCount {
		validate(t)
		return names[t]
	}
	return "unknown"
}

func UnreachableBoundary(t token) string {
	names := [...]string{"light", "dark"}
	if t < tokenCount && t == tokenCount {
		return names[t]
	}
	return "unknown"
}

func UnknownMidGuard(t token, valid bool) string {
	names := [...]string{"light", "dark"}
	if valid {
		return "unknown"
	}
	if tokenCount < t {
		return "unknown"
	}
	return names[t]
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
