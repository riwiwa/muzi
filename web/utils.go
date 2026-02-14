package web

// Functions used in the HTML templates

import (
	"fmt"
)

// Subtracts two integers
func sub(a int, b int) int {
	return a - b
}

// Adds two integers
func add(a int, b int) int {
	return a + b
}

// Put a comma in the thousands place, ten-thousands place etc.
func formatInt(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	} else {
		return formatInt(n/1000) + "," + fmt.Sprintf("%03d", n%1000)
	}
}
