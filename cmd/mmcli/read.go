package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"
)

const apiPageSize = 60

type channelMember struct {
	ChannelID    string `json:"channel_id"`
	UserID       string `json:"user_id"`
	LastViewedAt int64  `json:"last_viewed_at"`
}

type post struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
	CreateAt  int64  `json:"create_at"`
	DeleteAt  int64  `json:"delete_at"`
	Message   string `json:"message"`
}

type postList struct {
	Order []string        `json:"order"`
	Posts map[string]post `json:"posts"`
}

type boundary struct {
	Time int64  `json:"time"`
	ID   string `json:"id"`
}

type cursor struct {
	Scope string   `json:"scope"`
	After boundary `json:"after"`
}

type message struct {
	ID        string `json:"msg-id"`
	Timestamp string `json:"time-stamp"`
	ChannelID string `json:"channel-id"`
	Sender    string `json:"sender"`
	Text      string `json:"text"`
}

type readPage struct {
	Messages   []message `json:"messages"`
	NextCursor string    `json:"next-cursor,omitempty"`
}

func read(client *apiClient, me user, markerDir, scope string, channels []string, limit int, token string, out io.Writer) error {
	slices.Sort(channels)
	channels = slices.Compact(channels)
	cursorScope := digest(append([]string{scope}, channels...)...)
	after, err := decodeCursor(token, cursorScope)
	if err != nil {
		return err
	}
	markers, err := loadMarkers(markerDir)
	if err != nil {
		return err
	}
	members, err := client.memberships(me.ID, channels)
	if err != nil {
		return err
	}
	var posts []post
	for _, member := range members {
		channelPosts, err := client.unreadPosts(member, after, markers)
		if err != nil {
			return fmt.Errorf("read channel %q: %w", member.ChannelID, err)
		}
		posts = append(posts, channelPosts...)
	}
	slices.SortFunc(posts, func(a, b post) int {
		return compareBoundaries(postBoundary(a), postBoundary(b))
	})

	page := readPage{Messages: make([]message, 0)}
	if len(posts) > limit {
		posts = posts[:limit]
		encoded, err := json.Marshal(cursor{Scope: cursorScope, After: postBoundary(posts[limit-1])})
		if err != nil {
			return err
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(encoded)
	}

	senders := map[string]string{me.ID: me.Username}
	if me.Username == "" {
		senders[me.ID] = me.ID
	}
	for _, p := range posts {
		sender, cached := senders[p.UserID]
		if !cached {
			sender = client.senderName(p.UserID)
			senders[p.UserID] = sender
		}
		page.Messages = append(page.Messages, message{
			ID:        p.ID,
			Timestamp: time.UnixMilli(p.CreateAt).UTC().Format(time.RFC3339Nano),
			ChannelID: p.ChannelID,
			Sender:    sender,
			Text:      p.Message,
		})
	}
	return writeJSON(out, page)
}

func decodeCursor(token, scope string) (boundary, error) {
	if token == "" {
		return boundary{}, nil
	}
	var value cursor
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || json.Unmarshal(data, &value) != nil || value.After.ID == "" {
		return boundary{}, errors.New("invalid --cursor")
	}
	if value.Scope != scope {
		return boundary{}, errors.New("--cursor belongs to a different server, user or channel filter")
	}
	return value.After, nil
}

func postBoundary(p post) boundary {
	return boundary{Time: p.CreateAt, ID: p.ID}
}

func compareBoundaries(a, b boundary) int {
	if a.Time > b.Time || (a.Time == b.Time && a.ID > b.ID) {
		return -1
	}
	if a == b {
		return 0
	}
	return 1
}

func pageQuery(page int) url.Values {
	return url.Values{
		"page":     {strconv.Itoa(page)},
		"per_page": {strconv.Itoa(apiPageSize)},
	}
}

func (c *apiClient) memberships(userID string, channels []string) ([]channelMember, error) {
	var members []channelMember
	if len(channels) != 0 {
		for _, channelID := range channels {
			var member channelMember
			path := "/channels/" + pathID(channelID) + "/members/" + pathID(userID)
			if err := c.request(http.MethodGet, path, nil, nil, &member); err != nil {
				return nil, fmt.Errorf("get membership for channel %q: %w", channelID, err)
			}
			if member.ChannelID != channelID {
				return nil, errors.New("Mattermost returned an unexpected channel membership")
			}
			members = append(members, member)
		}
		return members, nil
	}
	path := "/users/" + pathID(userID) + "/channel_members"
	seen := make(map[string]bool)
	for page := 0; ; page++ {
		var batch []channelMember
		if err := c.request(http.MethodGet, path, pageQuery(page), nil, &batch); err != nil {
			return nil, fmt.Errorf("list channel memberships: %w", err)
		}
		for _, member := range batch {
			if member.ChannelID == "" {
				return nil, errors.New("Mattermost returned a membership without a channel ID")
			}
			if !seen[member.ChannelID] {
				members = append(members, member)
				seen[member.ChannelID] = true
			}
		}
		if len(batch) < apiPageSize {
			return members, nil
		}
	}
}

func (c *apiClient) unreadPosts(member channelMember, after boundary, markers map[string]bool) ([]post, error) {
	var posts []post
	seen := make(map[string]bool)
	path := "/channels/" + pathID(member.ChannelID) + "/posts"
	for page := 0; ; page++ {
		var batch postList
		query := pageQuery(page)
		query.Set("skipFetchThreads", "true")
		if err := c.request(http.MethodGet, path, query, nil, &batch); err != nil {
			return nil, err
		}
		for _, id := range batch.Order {
			p, ok := batch.Posts[id]
			if !ok || p.ID != id || p.ChannelID != member.ChannelID {
				return nil, errors.New("Mattermost returned an inconsistent post list")
			}
			if p.CreateAt <= member.LastViewedAt {
				return posts, nil
			}
			if p.DeleteAt != 0 || seen[id] || (after.ID != "" && compareBoundaries(postBoundary(p), after) <= 0) {
				continue
			}
			seen[id] = true
			if !markers[digest(id)] {
				posts = append(posts, p)
			}
		}
		if len(batch.Order) < apiPageSize {
			return posts, nil
		}
	}
}

func (c *apiClient) senderName(id string) string {
	if id == "" {
		return id
	}
	var sender user
	if err := c.request(http.MethodGet, "/users/"+pathID(id), nil, nil, &sender); err != nil || sender.Username == "" {
		return id
	}
	return sender.Username
}
