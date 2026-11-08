package errgroup

type Group struct {
	limit int
	err   error
}

func (g *Group) Go(f func() error) {
	g.err = f()
}

func (g *Group) TryGo(f func() error) bool {
	g.err = f()
	return true
}

func (g *Group) SetLimit(n int) {
	g.limit = n
}

func (g *Group) Wait() error {
	return g.err
}
