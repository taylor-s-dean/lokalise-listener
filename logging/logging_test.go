package logging

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

type exampleStruct struct {
	ExampleInt  int
	ExampleStr  string
	ExampleBool bool `json:"renamedBool"`
}

func TestJson(t *testing.T) {
	runTest := func(name string, value interface{}, expectedResult string) {
		t.Run(name, func(t *testing.T) {
			actualResult := Json(value)
			assert.Equal(t, expectedResult, actualResult)
		})
	}

	// Simple cases
	runTest("nil", nil, "null")
	runTest("int", 12, "12")
	runTest("bool", true, "true")
	runTest("string", "test", `"test"`)
	runTest("[]uint64", []uint64{3, 5, 8}, "[3,5,8]")
	runTest("struct", exampleStruct{}, `{"ExampleInt":0,"ExampleStr":"","renamedBool":false}`)
	runTest("map (int key)", map[int]string{0: "a", 3: "c"}, `{"0":"a","3":"c"}`)

	// Nested map
	runTest("nested map", map[string]interface{}{
		"a": "one",
		"b": 2,
		"c": map[string]interface{}{
			"d": nil,
		},
	}, `{"a":"one","b":2,"c":{"d":null}}`)

	// Bad value
	runTest("infinity", math.Inf(1), "<error: json: unsupported value: +Inf>")
	runTest("NaN", math.NaN(), "<error: json: unsupported value: NaN>")

	// Bad type
	runTest("function", func(string) int { return 0 }, "<error: json: unsupported type: func(string) int>")
	runTest("channel", make(chan int), "<error: json: unsupported type: chan int>")
	runTest("complex", complex(1, 1), "<error: json: unsupported type: complex128>")
	runTest("bool map", make(map[bool]int), "<error: json: unsupported type: map[bool]int>")

	// N.B.: this causes a stack overflow
	// circularMap := make(map[string]interface{})
	// circularMap["me"] = circularMap
	// runTest("circular map", circularMap, "")
}
