package main

import "fmt"

// Proto2Error is returned when the input file uses proto2 syntax.
type Proto2Error struct{}

func (e *Proto2Error) Error() string {
	return "proto2 files are not supported"
}

// ParseError wraps a parsing error from the scanner.
type ParseError struct {
	Err error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("parse error: %v", e.Err)
}

func (e *ParseError) Unwrap() error {
	return e.Err
}
