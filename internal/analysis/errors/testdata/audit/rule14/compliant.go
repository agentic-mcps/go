package rule14

type goodWrapper struct{ cause error }

func (goodWrapper) Error() string   { return "wrapped" }
func (w goodWrapper) Unwrap() error { return w.cause }

type notAnErrorWrapper struct{ cause error }

func (notAnErrorWrapper) Error() int { return 0 }
