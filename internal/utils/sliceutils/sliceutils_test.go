package sliceutils_test

type TestCase struct {
	A int
	B string
}

var (
	emptyTestCases []TestCase

	testCases = []TestCase{{
		A: 10,
		B: "hello",
	}, {
		A: 20,
		B: "bye",
	}}
)
