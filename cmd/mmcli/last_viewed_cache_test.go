package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestReadCheckpointsCompletedPoll(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	now := time.Now().UnixMilli()
	old := post{ID: "old", CreateAt: now - 2000, ChannelID: "channel", UserID: "me"}
	newer := post{ID: "newer", CreateAt: now - 1000, ChannelID: "channel", UserID: "me"}
	member := channelMember{ChannelID: "channel", UserID: "me", LastViewedAt: old.CreateAt - 1}
	posts := postList{Order: []string{newer.ID, old.ID}, Posts: map[string]post{old.ID: old, newer.ID: newer}}
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/v4/channels/channel/members/me":
			_ = json.NewEncoder(w).Encode(member)
		case "/api/v4/users/me/teams":
			_ = json.NewEncoder(w).Encode([]any{})
		case "/api/v4/channels/channel/posts":
			_ = json.NewEncoder(w).Encode(posts)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &apiClient{base: server.URL, host: "mm.example.org", http: server.Client()}
	me := user{ID: "me", Username: "me"}
	markerDir := t.TempDir()
	readOne := func(token string) readPage {
		t.Helper()
		var out bytes.Buffer
		if err := read(client, me, markerDir, "scope", []string{"channel"}, 1, token, &out); err != nil {
			t.Fatal(err)
		}
		var page readPage
		if err := json.Unmarshal(out.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}

	path := filepath.Join(home, ".mmcli", "cache", "mm.example.org", "last-viewed-at", "channel.txt")
	first := readOne("")
	if len(first.Messages) != 1 || first.Messages[0].ID != newer.ID || first.NextCursor == "" {
		t.Fatalf("unexpected first page: %+v", first)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("checkpoint before final page: %v", err)
	}
	poll, err := decodeCursor(first.NextCursor, digest("scope", "channel"))
	if err != nil {
		t.Fatal(err)
	}
	through := poll.Through
	arrived := post{ID: "arrived", CreateAt: through + 1, ChannelID: "channel", UserID: "me"}
	mu.Lock()
	posts.Order = append([]string{arrived.ID}, posts.Order...)
	posts.Posts[arrived.ID] = arrived
	mu.Unlock()
	last := readOne(first.NextCursor)
	if len(last.Messages) != 1 || last.Messages[0].ID != old.ID || last.NextCursor != "" {
		t.Fatalf("unexpected final page: %+v", last)
	}
	if stamp, err := loadLastViewed(path); err != nil || stamp != through {
		t.Fatalf("checkpoint = %d, %v, want %d", stamp, err, through)
	}
	mu.Lock()
	member.LastViewedAt--
	mu.Unlock()
	for time.Now().UnixMilli() <= arrived.CreateAt {
		time.Sleep(time.Millisecond)
	}
	repeat := readOne("")
	if len(repeat.Messages) != 1 || repeat.Messages[0].ID != arrived.ID || repeat.NextCursor != "" {
		t.Fatalf("unexpected next poll after server timestamp rollback: %+v", repeat)
	}
}
