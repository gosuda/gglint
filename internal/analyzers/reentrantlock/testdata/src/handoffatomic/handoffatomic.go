package atomic

import "sync"

func Store(mu *sync.Mutex) { mu.Lock() }
