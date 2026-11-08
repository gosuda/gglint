package typednil

import (
	"fmt"
	"io"
	"os"
)

type Err struct{ msg string }

func (e *Err) Error() string { return e.msg }

type Stringers []fmt.Stringer

type ErrPtr *Err

type Handler func()

func (h Handler) String() string { return "handler" }

type MyError = error

func pointerAsError() error {
	return (*Err)(nil) // want `returns typed nil \*Err as interface error: the result is never nil; return nil`
}

func parenthesizedResult() error {
	return ((*Err)(nil)) // want `returns typed nil \*Err as interface error: the result is never nil; return nil`
}

func parenthesizedNil() error {
	return (*Err)((nil)) // want `returns typed nil \*Err as interface error: the result is never nil; return nil`
}

func pointerAsAny() any {
	return (*Err)(nil) // want `returns typed nil \*Err as interface any: the result is never nil; return nil`
}

func foreignTypes() io.Closer {
	return (*os.File)(nil) // want `returns typed nil \*os.File as interface io.Closer: the result is never nil; return nil`
}

func secondResult() (int, error) {
	return 1, (*Err)(nil) // want `returns typed nil \*Err as interface error: the result is never nil; return nil`
}

func namedPointerAsAny() any {
	return ErrPtr(nil) // want `returns typed nil ErrPtr as interface any: the result is never nil; return nil`
}

func sliceAsAny() any {
	return []int(nil)
}

func mapAsAny() any {
	return map[string]int(nil)
}

func chanAsAny() any {
	return (chan int)(nil)
}

func funcAsStringer() fmt.Stringer {
	return Handler(nil)
}

func namedSliceAsAny() any {
	return Stringers(nil)
}

func aliasedInterface() MyError {
	return (*Err)(nil) // want `returns typed nil \*Err as interface MyError: the result is never nil; return nil`
}

func literalResult() func() error {
	return func() error {
		return (*Err)(nil) // want `returns typed nil \*Err as interface error: the result is never nil; return nil`
	}
}

func literalInsideConcreteFunc() *Err {
	check := func() error {
		return (*Err)(nil) // want `returns typed nil \*Err as interface error: the result is never nil; return nil`
	}
	_ = check
	return (*Err)(nil)
}

func literalConcreteInsideInterfaceFunc() error {
	get := func() *Err {
		return (*Err)(nil)
	}
	_ = get
	return nil
}

func plainNil() error {
	return nil
}

func freshPointer() error {
	return &Err{msg: "x"}
}

func concreteResult() *Err {
	return (*Err)(nil)
}

func concreteSlot() (*Err, error) {
	return (*Err)(nil), nil
}

func pointerVariable(p *Err) error {
	return p
}

func interfaceConversion() error {
	return error(nil)
}

func anyConversion() any {
	return any(nil)
}

func nonNilConversion(p *Err) error {
	return (*Err)(p)
}

func namedResult() (err error) {
	err = (*Err)(nil)
	return
}

func tupleCall() (int, error) {
	return pair()
}

func pair() (int, error) {
	return 0, nil
}

func typeParamResult[T any]() T {
	var zero T
	return zero
}

func typeParamSlot[T interface{ *Err }]() T {
	return T(nil)
}
