package rule06

import "context"

func Compliant(ctx context.Context) {
	go func() { _ = ctx }() // COMPLIANT: concurrency-06
}

func main() {
	go func() { _ = context.TODO() }()
}
