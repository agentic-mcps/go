package rule14

type wrapper struct{ cause error } // VIOLATION: errors-14

func (wrapper) Error() string { return "wrapped" }

var _ error = wrapper{}
