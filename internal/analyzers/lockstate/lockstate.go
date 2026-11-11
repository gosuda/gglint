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
	Doc:      "reports Unlock and RUnlock calls that panic on a mutex state this function sets with its own lock calls; a mutex passed to other code, a lock taken or released out of sight, and deferred calls are not checked",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runPair,
}

var upgradeAnalyzer = &analysis.Analyzer{
	Name:     "upgrade",
	Doc:      "reports Lock taken in this function while a read lock this function took is held; a read lock taken or released out of sight, including by a called helper, is not seen, and deferred calls are not checked",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runUpgrade,
}

var leakAnalyzer = &analysis.Analyzer{
	Name:     "leak",
	Doc:      "reports a return that leaves a mutex locked while another path of the same function unlocks it, including a lock handed to the caller; silent for a mutex with a deferred unlock, a local mutex never shared, or a mutex passed to other code; a helper of this package that unlocks counts as an unlock",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      runLeak,
}

var deferloopAnalyzer = &analysis.Analyzer{
	Name:     "deferloop",
	Doc:      "reports a deferred Unlock or RUnlock placed directly in a loop body after a direct Lock or RLock of the same mutex; nested blocks are not checked, and a body that unlocks the mutex itself is not reported",
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
