//go:build !coverage

package main

// coverageFlush is a no-op unless the binary is built with the coverage tag.
func coverageFlush() {}
