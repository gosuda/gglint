package secbounds

type token uint8

const (
	tokenLight token = iota
	tokenDark
	tokenCount
)

// UnguardedConstLeft would report G602 when the rule runs; with G602
// excluded the supplemental check must stay silent too.
func UnguardedConstLeft(t token) string {
	names := [...]string{"light", "dark"}
	if tokenCount < t {
		return "unknown"
	}
	return names[t]
}
