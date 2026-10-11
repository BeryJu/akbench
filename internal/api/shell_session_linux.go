/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package api

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// killSessionMembers SIGKILLs every process still in session sid. Killing
// the shell's process group alone misses jobs bash moved into groups of
// their own (`kubectl logs -f ... &`, anything nohup'd), which would
// otherwise outlive the browser tab for as long as the pod runs.
func killSessionMembers(sid int) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if processSession(pid) == sid {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}

// processSession reads field 6 (session) of /proc/<pid>/stat. The command
// name (field 2) is parenthesised and may itself contain spaces or
// parentheses, so fields are counted from the last ')'.
func processSession(pid int) int {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return -1
	}
	stat := string(raw)
	end := strings.LastIndexByte(stat, ')')
	if end < 0 {
		return -1
	}
	// After ')': state ppid pgrp session ...
	fields := strings.Fields(stat[end+1:])
	if len(fields) < 4 {
		return -1
	}
	sid, err := strconv.Atoi(fields[3])
	if err != nil {
		return -1
	}
	return sid
}
