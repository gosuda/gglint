package handoffext

import "sync"

func Lock(mu *sync.Mutex) { mu.Lock() }
