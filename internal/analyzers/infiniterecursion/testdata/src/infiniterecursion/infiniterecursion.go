package infiniterecursion

import "os"

func read(n int) int { return n }

func direct(n int) int { return direct(n) } // want "function direct calls itself with its own arguments on every path"

func viaArg(n int) int { return read(viaArg(n)) } // want "function viaArg calls itself with its own arguments on every path"

func leftOperand(n int) bool { return leftOperand(n) && n > 0 } // want "function leftOperand calls itself with its own arguments on every path"

func shortCircuit(n int) bool { return n > 0 && shortCircuit(n) }

func variadic(xs ...int) int { return variadic(xs...) } // want "function variadic calls itself with its own arguments on every path"

func inCondition(n int) bool { // want "function inCondition calls itself with its own arguments on every path"
	if inCondition(n) {
		return true
	}
	return false
}

func boundedLoop(n int) int { // want "function boundedLoop calls itself with its own arguments on every path"
	for range n {
	}
	return boundedLoop(n)
}

func literal(n int) int { // want "function literal calls itself with its own arguments on every path"
	f := func() int { return 0 }
	_ = f
	return literal(n)
}

func typeSwitch(v any) int { // want "function typeSwitch calls itself with its own arguments on every path"
	switch v.(type) {
	case int:
		return typeSwitch(v)
	default:
		return typeSwitch(v)
	}
}

func typeSwitchNoDefault(v any) int {
	switch v.(type) {
	case int:
		return typeSwitchNoDefault(v)
	}
	return 0
}

func generic[T any](v T) T { return generic[T](v) } // want "function generic calls itself with its own arguments on every path"

func inferred[T any](v T) T { return inferred(v) } // want "function inferred calls itself with its own arguments on every path"

func otherInstance[T any](v T) any { return otherInstance[any](v) }

type counter struct{ n int }

func (c counter) next(n int) int { return c.next(n) } // want "function next calls itself with its own arguments on every path"

func (c counter) field(n int) int {
	c.n = n
	return c.field(n)
}

func (c *counter) copied() {
	d := c
	d.copied()
}

func (counter) unnamed(n int) int { return counter{}.unnamed(n) }

func blank(_ int) int { return blank(0) }

func shadowed(n int) int {
	{
		n := 1
		return shadowed(n)
	}
}

func addressed(n int) int {
	p := &n
	_ = p
	return addressed(n)
}

func ranged(n int, xs []int) int {
	for n = range xs {
	}
	return ranged(n, xs)
}

func panics(n int) int {
	if n > 0 {
		panic("stop")
	}
	return panics(n)
}

func exits(n int) int {
	if n > 0 {
		os.Exit(1)
	}
	return exits(n)
}

func spin(n int) int {
	for {
	}
	return spin(n)
}

func ifInit(n int) int {
	if m := n; m > 0 {
		return ifInit(n)
	} else {
		return ifInit(n)
	}
}

func mutual(n int) int { return read(n) + mutualPeer(n) }

func mutualPeer(n int) int { return mutual(n) }
