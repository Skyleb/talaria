//go:build !windows

package main

import "errors"

func isWindowsService() bool { return false }

func runWindowsService(func(stop <-chan struct{})) {}

func serviceControl(string, string) error {
	return errors.New("-service is only available on Windows; use the systemd unit in deploy/ on Linux")
}
