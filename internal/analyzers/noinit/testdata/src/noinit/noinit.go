package noinit

var ready bool

func init() { // want `init function: move setup into an explicit function called from main`
	ready = true
}

func init() { // want `init function: move setup into an explicit function called from main`
	ready = !ready
}

type service struct{}

func (service) init() {}

func (*service) Init() {}

func initialize() {}

var initHook = func() {}

func setup() {
	init := func() {}
	init()
	initialize()
	initHook()
}
