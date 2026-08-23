package rule02

type Worker struct{}

func (w *Worker) Start() { go func() {}() } // VIOLATION: concurrency-02
