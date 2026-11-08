package interfaces

type Three interface {
	A()
	B()
	C()
}

type Four interface { // want `^interface Four is too large: method count 4 exceeds 3$`
	A()
	B()
	C()
	D()
}

type Embeds interface {
	Three
	D()
}

type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

type Generic[T any] interface { // want `^interface Generic is too large: method count 4 exceeds 3$`
	A(T)
	B(T)
	C(T)
	D(T)
}

type Alias = interface {
	A()
	B()
	C()
	D()
}

var anonymous interface {
	A()
	B()
	C()
	D()
}

func local() {
	type inner interface { // want `^interface inner is too large: method count 4 exceeds 3$`
		A()
		B()
		C()
		D()
	}
	var _ inner
}
