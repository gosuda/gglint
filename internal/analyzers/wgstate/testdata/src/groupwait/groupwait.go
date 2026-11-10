package groupwait

import "golang.org/x/sync/errgroup"

func work() error { return nil }

func groupwaitReported() error {
	var g errgroup.Group
	g.Go(func() error {
		return g.Wait() // want "so it waits for itself"
	})
	return g.Wait()
}

func groupwaitSilent() error {
	var g errgroup.Group
	g.Go(func() error {
		return work()
	})
	return g.Wait()
}

func groupwaitSilentNested() error {
	var g errgroup.Group
	g.Go(func() error {
		fn := func() error {
			return g.Wait()
		}
		return fn()
	})
	return g.Wait()
}
func groupwaitSilentGoWait() error {
	var g errgroup.Group
	g.Go(func() error {
		go g.Wait()
		return nil
	})
	return g.Wait()
}

func groupwaitNoBody() int
