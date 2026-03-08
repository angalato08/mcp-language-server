package main

import "fmt"

// LeafFunction is at the bottom of the call chain — called but calls nothing interesting
func LeafFunction() string {
	return "leaf result"
}

// MiddleFunction calls LeafFunction and is called by EntryPoint
func MiddleFunction() string {
	result := LeafFunction()
	return fmt.Sprintf("middle: %s", result)
}

// EntryPoint is the top of the call chain — calls MiddleFunction
func EntryPoint() {
	msg := MiddleFunction()
	fmt.Println(msg)
}

// AnotherCaller also calls MiddleFunction, giving it two incoming callers
func AnotherCaller() {
	fmt.Println(MiddleFunction())
}
