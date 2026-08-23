package rule04

import "log"

func testOnly() error {
	var err error
	if err != nil {
		log.Print(err)
		return err
	}
	return nil
}
