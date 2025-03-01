// Go support for leveled logs, analogous to https://code.google.com/p/google-glog/
//
// Copyright 2013 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// File I/O for logs.

package glog

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// log rotation size (initialized via LoadConfig; any change requires restart)
var MaxSize uint64 = 1024 * 1024 * 1024

var (
	// logDirs lists the candidate directories for new log files.
	logDirs []string
	// If non-empty, overrides the choice of directory in which to write logs.
	// See createLogDirs for the full list of possible destinations.
	logDir string
)

var (
	pid      int
	program  string
	aisrole  string
	host     = "unknownhost"
	userName = "unknownuser"
)

var FileHeaderCB func() string

var onceInitFiles sync.Once

func init() {
	pid = os.Getpid()
	program = filepath.Base(os.Args[0])

	h, err := os.Hostname()
	if err == nil {
		host = shortHostname(h)
	}

	current, err := user.Current()
	if err == nil {
		userName = current.Username
	}

	// Sanitize userName since it may contain filepath separators on Windows.
	userName = strings.ReplaceAll(userName, `\`, "_")
}

func InitFlags(flset *flag.FlagSet) {
	flset.BoolVar(&logging.toStderr, "logtostderr", false, "log to standard error instead of files")
	flset.BoolVar(&logging.alsoToStderr, "alsologtostderr", false, "log to standard error as well as files")
}

func initFiles() {
	if logDir != "" {
		logDirs = append(logDirs, logDir)
	}
	logDirs = append(logDirs, filepath.Join(os.TempDir(), "aislogs"))
	if err := logging.createFiles(errorLog); err != nil {
		panic(err)
	}
}

func SetLogDirRole(dir, role string) { logDir, aisrole = dir, role }

func shortProgram() (prog string) {
	prog = program
	if prog == "aisnode" && aisrole != "" {
		prog = "ais" + aisrole
	}
	return
}

func InfoLogName() string { return shortProgram() + ".INFO" }
func WarnLogName() string { return shortProgram() + ".WARNING" }
func ErrLogName() string  { return shortProgram() + ".ERROR" }

// shortHostname returns its argument, truncating at the first period.
// For instance, given "www.google.com" it returns "www".
func shortHostname(hostname string) string {
	if i := strings.Index(hostname, "."); i >= 0 {
		return hostname[:i]
	}
	if len(hostname) < 16 || strings.IndexByte(hostname, '-') < 0 {
		return hostname
	}
	// shorten even more (e.g. "runner-r9rhlq8--project-4149-concurrent-0")
	parts := strings.Split(hostname, "-")
	if len(parts) < 2 {
		return hostname
	}
	if parts[1] != "" || len(parts) == 2 {
		return parts[0] + "-" + parts[1]
	}
	return parts[0] + "-" + parts[2]
}

// logName returns a new log file name containing tag, with start time t, and
// the name for the symlink for tag.
func logName(tag string, t time.Time) (name, link string) {
	prog := shortProgram()
	name = fmt.Sprintf("%s.%s.%s.%02d%02d-%02d%02d%02d.%d",
		prog,
		host,
		tag,
		t.Month(),
		t.Day(),
		t.Hour(),
		t.Minute(),
		t.Second(),
		pid)
	return name, prog + "." + tag
}

// create creates a new log file and returns the file and its filename, which
// contains tag ("INFO", "FATAL", etc.) and t.  If the file is created
// successfully, create also attempts to update the symlink for that tag, ignoring
// errors.
func create(tag string, t time.Time) (f *os.File, filename string, err error) {
	if len(logDirs) == 0 {
		return nil, "", errors.New("no log dirs")
	}
	name, link := logName(tag, t)
	var lastErr error
	for _, dir := range logDirs {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			lastErr = nil
			continue
		}

		fname := filepath.Join(dir, name)
		f, err := os.OpenFile(fname, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
		if err != nil {
			lastErr = err
			continue
		}
		symlink := filepath.Join(dir, link)
		os.Remove(symlink)        // ignore err
		os.Symlink(name, symlink) // ignore err
		return f, fname, nil
	}
	return nil, "", fmt.Errorf("cannot create log: %v", lastErr)
}
