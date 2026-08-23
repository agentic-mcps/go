package rule01

import "context"

func Compliant(ctx context.Context) {
	go func() {
		select {
		case <-ctx.Done():
			return
		default:
		}
	}() // COMPLIANT: concurrency-01
}
