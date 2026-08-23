package rule09

func init() { // VIOLATION: concurrency-09
	go workViolation()
}

func workViolation() {}
