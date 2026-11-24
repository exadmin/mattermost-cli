package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const channelCacheTTL = time.Hour

type channelCache struct {
	Scope  string      `json:"scope"`
	Result channelList `json:"result"`
}

func (c *apiClient) channelList(force bool) (channelList, error) {
	path, err := channelCachePath(c.host)
	if err != nil {
		return channelList{}, err
	}
	scope := digest(c.base, c.token)
	if !force {
		if result, ok := readChannelCache(path, scope, channelCacheTTL); ok {
			return result, nil
		}
	}

	result, err := c.fetchChannelList()
	if err != nil {
		return channelList{}, err
	}
	if err := saveChannelCache(path, channelCache{Scope: scope, Result: result}); err != nil {
		return channelList{}, fmt.Errorf("save channel cache: %w", err)
	}
	return result, nil
}

func channelCachePath(host string) (string, error) {
	dir, err := channelCacheDirectory(host)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "list-channels.json"), nil
}

func channelCacheDirectory(host string) (string, error) {
	domain := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, strings.ToLower(host))
	domain = strings.TrimRight(domain, ".")
	if domain == "" {
		return "", errors.New("settings URL hostname cannot be used for the channel cache")
	}
	if !filepath.IsLocal(domain) {
		// Windows reserves names such as CON, even with a domain suffix.
		domain = "-" + domain
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory for channel cache: %w", err)
	}
	return filepath.Join(home, ".mmcli", "cache", domain), nil
}

func readChannelCache(path, scope string, maxAge time.Duration) (channelList, bool) {
	file, err := os.Open(path)
	if err != nil {
		return channelList{}, false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return channelList{}, false
	}
	age := time.Since(info.ModTime())
	if maxAge > 0 && (age < 0 || age >= maxAge) {
		return channelList{}, false
	}
	var cache channelCache
	data, err := io.ReadAll(file)
	if err != nil || json.Unmarshal(data, &cache) != nil ||
		cache.Scope != scope || cache.Result.Channels == nil {
		return channelList{}, false
	}
	return cache.Result, true
}

func saveChannelCache(path string, cache channelCache) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".list-channels-*.tmp")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)

	writeErr := writeJSON(file, cache)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
