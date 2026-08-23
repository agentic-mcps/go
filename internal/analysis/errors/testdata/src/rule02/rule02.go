package rule02

type problem struct{}

func (problem) Error() string { return "problem" }

func Bad() problem  { return problem{} } // want "exported function Bad returns concrete error type"
func good() problem { return problem{} }
func Good() error   { return nil }
