package rule02

type CompliantWorker struct{}

func (w *CompliantWorker) Start() { go func() {}() } // COMPLIANT: concurrency-02
func (w *CompliantWorker) Stop()  {}

type GenericWorker[T any] struct{}

func (w *GenericWorker[T]) Start()    { go func() {}() }
func (w *GenericWorker[T]) Shutdown() {}
