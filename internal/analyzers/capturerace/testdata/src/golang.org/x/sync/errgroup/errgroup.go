package errgroup

type Group struct {
	limit int
}

func (g *Group) Go(f func() error) {}

func (g *Group) TryGo(f func() error) bool { return true }

func (g *Group) SetLimit(n int) { g.limit = n }

func (g *Group) Wait() error { return nil }
