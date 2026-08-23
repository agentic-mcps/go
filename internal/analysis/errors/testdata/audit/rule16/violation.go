package rule16

type resource struct{}

func (resource) Close() error { return nil }
func Bad(r resource) (err error) {
	defer r.Close() // VIOLATION: errors-16
	return nil
}
