package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

type threadView struct {
	ID           string `json:"id"`
	LastViewedAt int64  `json:"last_viewed_at"`
	LastReplyAt  int64  `json:"last_reply_at"`
	Post         struct {
		ChannelID string `json:"channel_id"`
	} `json:"post"`
}

func (c *apiClient) threadViews(userID string, members []channelMember) (map[string]map[string]threadView, error) {
	views := make(map[string]map[string]threadView, len(members))
	if len(members) == 0 {
		return views, nil
	}
	for _, member := range members {
		views[member.ChannelID] = make(map[string]threadView)
	}
	teams, err := c.userTeamIDs(userID)
	if err != nil {
		return nil, err
	}
	for _, teamID := range teams {
		if err := c.loadTeamThreadViews(userID, teamID, views); err != nil {
			return nil, fmt.Errorf("list followed threads for team %q: %w", teamID, err)
		}
	}
	return views, nil
}

func (c *apiClient) loadTeamThreadViews(userID, teamID string, views map[string]map[string]threadView) error {
	path := "/users/" + pathID(userID) + "/teams/" + pathID(teamID) + "/threads"
	query := url.Values{
		"per_page":    {strconv.Itoa(apiPageSize)},
		"threadsOnly": {"true"},
	}
	seenCursors := make(map[string]bool)
	for {
		var page struct {
			Threads []threadView `json:"threads"`
		}
		if err := c.request(http.MethodGet, path, query, nil, &page); err != nil {
			return err
		}
		for _, thread := range page.Threads {
			if channelViews, selected := views[thread.Post.ChannelID]; selected && thread.ID != "" {
				channelViews[thread.ID] = thread
			}
		}
		if len(page.Threads) < apiPageSize {
			return nil
		}
		before := page.Threads[len(page.Threads)-1].ID
		if before == "" || seenCursors[before] {
			return errors.New("Mattermost returned an invalid thread page cursor")
		}
		seenCursors[before] = true
		query.Set("before", before)
	}
}
