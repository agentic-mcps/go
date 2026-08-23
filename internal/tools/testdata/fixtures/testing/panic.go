package testingfixture

func Indexed(values []int, index int) int {
	if index == -1 {
		return 0
	}
	return values[index]
}
