package rule07

import "errors"

func Bad() error { return errors.New("Invalid value!") } // VIOLATION: errors-07
