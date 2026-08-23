package rule15

func (s *State) Violation() { s.a.Add(1); s.b.Store(1) } // VIOLATION: concurrency-15
