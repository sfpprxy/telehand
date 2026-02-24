//go:build !windows

package main

func isCurrentUserAdmin() (bool, error) {
	return true, nil
}
