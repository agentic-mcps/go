package rule11

func MustOther() int { return 1 }
func MustBuild() int { return MustOther() }
