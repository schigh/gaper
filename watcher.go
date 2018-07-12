package gaper

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	zglob "github.com/mattn/go-zglob"
)

// Watcher is a interface for the watch process
type Watcher interface {
	Watch()
	Errors() chan error
	Events() chan string
}

// watcher is a interface for the watch process
type watcher struct {
	pollInterval      int
	watchItems        map[string]bool
	ignoreItems       map[string]bool
	allowedExtensions map[string]bool
	events            chan string
	errors            chan error
}

// NewWatcher creates a new watcher
func NewWatcher(pollInterval int, watchItems []string, ignoreItems []string, extensions []string) (Watcher, error) {
	if pollInterval == 0 {
		pollInterval = DefaultPoolInterval
	}

	if len(extensions) == 0 {
		extensions = DefaultExtensions
	}

	allowedExts := make(map[string]bool)
	for _, ext := range extensions {
		allowedExts["."+ext] = true
	}

	watchPaths, err := resolvePaths(watchItems, allowedExts)
	if err != nil {
		return nil, err
	}

	ignorePaths, err := resolvePaths(ignoreItems, allowedExts)
	if err != nil {
		return nil, err
	}

	logger.Debugf("Resolved watch paths: %v", watchPaths)
	logger.Debugf("Resolved ignore paths: %v", ignorePaths)
	return &watcher{
		events:            make(chan string),
		errors:            make(chan error),
		pollInterval:      pollInterval,
		watchItems:        watchPaths,
		ignoreItems:       ignorePaths,
		allowedExtensions: allowedExts,
	}, nil
}

var startTime = time.Now()
var errDetectedChange = errors.New("done")

// Watch starts watching for file changes
func (w *watcher) Watch() {
	for {
		for watchPath := range w.watchItems {
			fileChanged, err := w.scanChange(watchPath)
			if err != nil {
				w.errors <- err
				return
			}

			if fileChanged != "" {
				w.events <- fileChanged
				startTime = time.Now()
			}
		}

		time.Sleep(time.Duration(w.pollInterval) * time.Millisecond)
	}
}

// Events get events occurred during the watching
// these events are emited only a file changing is detected
func (w *watcher) Events() chan string {
	return w.events
}

// Errors get errors occurred during the watching
func (w *watcher) Errors() chan error {
	return w.errors
}

func (w *watcher) scanChange(watchPath string) (string, error) {
	logger.Debug("Watching ", watchPath)

	var fileChanged string

	err := filepath.Walk(watchPath, func(path string, info os.FileInfo, err error) error {
		// always ignore hidden files and directories
		if dir := filepath.Base(path); dir[0] == '.' && dir != "." {
			return skipFile(info)
		}

		if _, ignored := w.ignoreItems[path]; ignored {
			return skipFile(info)
		}

		ext := filepath.Ext(path)
		if _, ok := w.allowedExtensions[ext]; ok && info.ModTime().After(startTime) {
			fileChanged = path
			return errDetectedChange
		}

		return nil
	})

	if err != nil && err != errDetectedChange {
		return "", err
	}

	return fileChanged, nil
}

func resolvePaths(paths []string, extensions map[string]bool) (map[string]bool, error) {
	result := map[string]bool{}

	for _, path := range paths {
		matches := []string{path}

		isGlob := strings.Contains(path, "*")
		if isGlob {
			var err error
			matches, err = zglob.Glob(path)
			if err != nil {
				return nil, fmt.Errorf("couldn't resolve glob path \"%s\": %v", path, err)
			}
		}

		for _, match := range matches {
			// don't care for extension filter right now for non glob paths
			// since they could be a directory
			if isGlob {
				if _, ok := extensions[filepath.Ext(path)]; !ok {
					continue
				}
			}

			if _, ok := result[match]; !ok {
				result[match] = true
			}
		}
	}

	removeOverlappedPaths(result)

	return result, nil
}

// remove overlapped paths so it makes the scan for changes later faster and simpler
func removeOverlappedPaths(mapPaths map[string]bool) {
	for p1 := range mapPaths {
		// skip to next item if this path has already been checked
		if v, ok := mapPaths[p1]; ok && !v {
			continue
		}

		for p2 := range mapPaths {
			if p1 == p2 {
				continue
			}

			if strings.HasPrefix(p2, p1) {
				mapPaths[p2] = false
			} else if strings.HasPrefix(p1, p2) {
				mapPaths[p1] = false
			}
		}
	}

	// cleanup path list
	for p := range mapPaths {
		if !mapPaths[p] {
			delete(mapPaths, p)
		}
	}
}

func skipFile(info os.FileInfo) error {
	if info.IsDir() {
		return filepath.SkipDir
	}
	return nil
}
