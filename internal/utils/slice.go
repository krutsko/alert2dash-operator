package utils

import "slices"

// HasString checks if a string exists in a slice
func HasString(slice []string, s string) bool {
	return slices.Contains(slice, s)
}

// RemoveString removes a string from a slice
func RemoveString(slice []string, s string) []string {
	return slices.DeleteFunc(slice, func(item string) bool {
		return item == s
	})
}
