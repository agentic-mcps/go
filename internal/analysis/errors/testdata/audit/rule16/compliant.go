package rule16

type safeResource struct{}

func (safeResource) Close()     {}
func Good(r safeResource) error { defer r.Close(); return nil }

func GoodCapture(r resource) (err error) {
	defer func() {
		if closeErr := r.Close(); err == nil {
			err = closeErr
		}
	}()
	return nil
}

func NestedNoErrorReturn(r resource) error {
	func() { defer r.Close() }()
	return nil
}
