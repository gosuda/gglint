package params

type T struct{}

type Flag bool

func three(a, b int, c string) {}

func four(a, b int, c, d string) {} // want `^function four is too complex: parameter count 4 exceeds 3$`

func unnamedThree(int, string, float64) {}

func unnamedFour(int, string, float64, byte) {} // want `^function unnamedFour is too complex: parameter count 4 exceeds 3$`

func blanks(_, _ int, _ string, _ byte) {} // want `^function blanks is too complex: parameter count 4 exceeds 3$`

func variadic(a, b int, rest ...int) {}

func (T) receiverExcluded(a, b, c int) {}

func (t *T) receiverPointer(a, b, c, d int) {} // want `^function receiverPointer is too complex: parameter count 4 exceeds 3$`

var literal = func(a, b, c, d int) {} // want `^function literal is too complex: parameter count 4 exceeds 3$`

func OneBool(a bool, n int) {}

func TwoBools(a, b bool) {} // want `^function TwoBools is too complex: bool parameter count 2 exceeds 1$`

func UnnamedBools(bool, bool) {} // want `^function UnnamedBools is too complex: bool parameter count 2 exceeds 1$`

func BoolVariadic(a bool, rest ...bool) {}

func NamedBoolType(a, b Flag) {}

func twoBoolsUnexported(a, b bool) {}

func (T) TwoBoolsMethod(a, b bool) {}

var boolLiteral = func(a, b bool) {}

func Both(a, b, c, d bool) {} // want `^function Both is too complex: parameter count 4 exceeds 3, bool parameter count 4 exceeds 1$`
