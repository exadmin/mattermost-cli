package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

func digest(parts ...string) string {
	data, _ := json.Marshal(parts)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func markerDirectory(scope string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user configuration directory: %w", err)
	}
	return filepath.Join(dir, "mmcli", "read-markers", scope), nil
}

func markRead(client *apiClient, dir string, ids []string, out io.Writer) error {
	result := markReadResult{ProcessedIDs: make([]string, 0, len(ids))}
	for _, id := range ids {
		if err := markMessage(client, dir, id); err != nil {
			result.MessageID = id
			return &commandError{Message: err.Error(), markReadResult: &result}
		}
		result.ProcessedIDs = append(result.ProcessedIDs, id)
	}
	return writeJSON(out, result)
}

func markMessage(client *apiClient, dir, id string) error {
	var message post
	if err := client.request(http.MethodGet, "/posts/"+pathID(id), nil, nil, &message); err != nil {
		return err
	}
	if message.ID != id || message.DeleteAt != 0 {
		return errors.New("Mattermost did not return the requested accessible message")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create marker directory: %w", err)
	}

	// Each marker is an empty file. Exclusive creation preserves concurrent updates.
	file, err := os.OpenFile(filepath.Join(dir, digest(id)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("save read marker: %w", err)
	}
	return file.Close()
}

func loadMarkers(dir string) (map[string]bool, error) {
	markers := make(map[string]bool)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return markers, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load read markers: %w", err)
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			markers[entry.Name()] = true
		}
	}
	return markers, nil
}
