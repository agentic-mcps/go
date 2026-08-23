package rule04

func Compliant(in <-chan int, out chan<- int) {} // COMPLIANT: concurrency-04
