package rule07

import "errors"

func Good() error { return errors.New("invalid value") }
