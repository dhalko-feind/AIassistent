//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
)

func showMessage(title, text string) { fmt.Fprintf(os.Stderr, "%s: %s\n", title, text) }

func openBrowser(url string) error { return exec.Command("xdg-open", url).Start() }

func openFolder(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	if _, err := exec.LookPath("xdg-open"); err != nil {
		return fmt.Errorf("kein Dateimanager verfügbar (%s)", path)
	}
	return exec.Command("xdg-open", path).Start()
}

func fixedDrives() []string { return nil }
