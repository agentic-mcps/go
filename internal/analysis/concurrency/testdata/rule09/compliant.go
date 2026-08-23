package rule09

func Start() { // COMPLIANT: concurrency-09
	go work()
}

func work() {}
