package lockstate

import (
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"
)

// Analyzers returns the six lockstate check analyzers.
func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		pairAnalyzer,
		upgradeAnalyzer,
		leakAnalyzer,
		deferloopAnalyzer,
		trylockAnalyzer,
		selfcopyAnalyzer,
	}
}

var pairAnalyzer = &analysis.Analyzer{
	Name:     "pair",
	Doc:      "reports Unlock and RUnlock calls that panic on the current lock state",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runPair,
}

var upgradeAnalyzer = &analysis.Analyzer{
	Name:     "upgrade",
	Doc:      "reports Lock taken while the goroutine holds a read lock",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runUpgrade,
}

var leakAnalyzer = &analysis.Analyzer{
	Name:     "leak",
	Doc:      "reports returns that leave a mutex locked on one path but not another",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runLeak,
}

var deferloopAnalyzer = &analysis.Analyzer{
	Name:     "deferloop",
	Doc:      "reports deferred unlocks inside loop bodies that hold the lock across iterations",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runDeferloop,
}

var trylockAnalyzer = &analysis.Analyzer{
	Name:     "trylock",
	Doc:      "reports TryLock results that are dropped without branching",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runTrylock,
}

var selfcopyAnalyzer = &analysis.Analyzer{
	Name:     "selfcopy",
	Doc:      "reports locks on a copy taken through a value receiver",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runSelfcopy,
}
