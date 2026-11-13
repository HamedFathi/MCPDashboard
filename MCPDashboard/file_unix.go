//go:build !windows

package main

import (
	"os"
	"os/exec"
)

func replaceFile(source, target string) error { return os.Rename(source, target) }
func configureConsole(*exec.Cmd)              {}
