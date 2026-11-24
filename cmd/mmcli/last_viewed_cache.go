package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func lastViewedCachePath(cacheDir, channelID string) (string, error) {
	name := channelID + ".txt"
	if channelID == "" || !filepath.IsLocal(name) || strings.ContainsAny(name, `/\:`) {
		return "", fmt.Errorf("invalid channel ID for last-viewed-at cache: %q", channelID)
	}
	return filepath.Join(cacheDir, "last-viewed-at", name), nil
}

func loadLastViewed(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read last-viewed-at cache %q: %w", path, err)
	}
	stamp, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || stamp <= 0 {
		return 0, nil
	}
	return stamp, nil
}

func saveLastViewed(path string, stamp int64) error {
	current, err := loadLastViewed(path)
	if err != nil || current >= stamp {
		return err
	}
	if err := replaceLastViewed(path, stamp); err != nil {
		return fmt.Errorf("save last-viewed-at cache %q: %w", path, err)
	}
	return nil
}

func replaceLastViewed(path string, stamp int64) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".last-viewed-at-*.tmp")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	_, writeErr := fmt.Fprintln(file, stamp)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
