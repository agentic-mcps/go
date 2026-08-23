package rule02

type problem struct{}

func (problem) Error() string { return "problem" }
func Bad() problem            { return problem{} } // VIOLATION: errors-02
