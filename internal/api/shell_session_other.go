//go:build !linux

package api

// killSessionMembers is Linux-only (it walks /proc); elsewhere - local
// development on macOS - killing the shell's own process group has to do.
func killSessionMembers(int) {}
