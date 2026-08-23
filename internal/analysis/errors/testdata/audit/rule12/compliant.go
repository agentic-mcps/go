package rule12

import "strings"

func Good(err error) bool { return strings.HasPrefix(err.Error(), "missing") }
