package rule05

import "fmt"

func Good(err error) error {
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	return nil
}
