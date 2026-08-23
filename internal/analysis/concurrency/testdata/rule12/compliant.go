package rule12

import "sync"

func Compliant() { var wg sync.WaitGroup; go func() { wg.Done() }() }
