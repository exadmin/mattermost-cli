package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

type channel struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	DeleteAt    int64  `json:"delete_at"`
}

type listedChannel struct {
	ID         string `json:"channel-id"`
	Visibility string `json:"visibility"`
	VisualName string `json:"visual-name"`
}

func listChannels(client *apiClient, me user, out io.Writer) error {
	channels, err := client.availableChannels(me.ID)
	if err != nil {
		return err
	}

	ids := make([]string, 0, len(channels))
	for id := range channels {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	names := map[string]string{me.ID: me.visualName()}
	result := struct {
		Channels []listedChannel `json:"channels"`
	}{Channels: make([]listedChannel, 0, len(channels))}
	for _, id := range ids {
		ch := channels[id]
		visibility, name := "private", ch.DisplayName
		switch ch.Type {
		case "O":
			visibility = "public"
		case "P":
		case "D", "G":
			if ch.Type == "D" || name == "" {
				name, err = client.chatName(ch, me.ID, names)
				if err != nil {
					return fmt.Errorf("name chat %q: %w", ch.ID, err)
				}
			}
		default:
			continue
		}
		if name == "" {
			name = ch.Name
		}
		if name == "" {
			name = ch.ID
		}
		result.Channels = append(result.Channels, listedChannel{ID: id, Visibility: visibility, VisualName: name})
	}
	return writeJSON(out, result)
}

func (c *apiClient) availableChannels(userID string) (map[string]channel, error) {
	userPath := "/users/" + pathID(userID)
	var joined []channel
	if err := c.request(http.MethodGet, userPath+"/channels", nil, nil, &joined); err != nil {
		return nil, fmt.Errorf("list joined channels: %w", err)
	}
	var teams []struct {
		ID       string `json:"id"`
		DeleteAt int64  `json:"delete_at"`
	}
	if err := c.request(http.MethodGet, userPath+"/teams", nil, nil, &teams); err != nil {
		return nil, fmt.Errorf("list user teams: %w", err)
	}

	channels := make(map[string]channel, len(joined))
	for _, ch := range joined {
		if ch.ID != "" && ch.DeleteAt == 0 {
			channels[ch.ID] = ch
		}
	}
	for _, team := range teams {
		if team.ID == "" || team.DeleteAt != 0 {
			continue
		}
		if err := c.addPublicChannels(team.ID, channels); err != nil {
			return nil, fmt.Errorf("list public channels for team %q: %w", team.ID, err)
		}
	}
	return channels, nil
}

func (c *apiClient) addPublicChannels(teamID string, channels map[string]channel) error {
	path := "/teams/" + pathID(teamID) + "/channels"
	for page := 0; ; page++ {
		var batch []channel
		err := c.request(http.MethodGet, path, pageQuery(page), nil, &batch)
		if errors.Is(err, apiStatusError(http.StatusForbidden)) {
			// This team does not allow the current user to browse public channels.
			return nil
		}
		if err != nil {
			return err
		}
		for _, ch := range batch {
			if ch.ID != "" && ch.Type == "O" && ch.DeleteAt == 0 {
				channels[ch.ID] = ch
			}
		}
		if len(batch) < apiPageSize {
			return nil
		}
	}
}

func (c *apiClient) chatName(ch channel, myID string, names map[string]string) (string, error) {
	var participants []string
	selfChat := false
	path := "/channels/" + pathID(ch.ID) + "/members"
	for page := 0; ; page++ {
		var members []channelMember
		if err := c.request(http.MethodGet, path, pageQuery(page), nil, &members); err != nil {
			return "", err
		}
		for _, member := range members {
			switch member.UserID {
			case myID:
				selfChat = true
			case "":
			default:
				participants = append(participants, member.UserID)
			}
		}
		if len(members) < apiPageSize {
			break
		}
	}
	if len(participants) == 0 && selfChat && ch.Type == "D" {
		return names[myID], nil
	}

	slices.Sort(participants)
	participants = slices.Compact(participants)
	displayNames := make([]string, 0, len(participants))
	for _, id := range participants {
		name, ok := names[id]
		if !ok {
			profile := user{ID: id}
			if err := c.request(http.MethodGet, "/users/"+pathID(id), nil, nil, &profile); err != nil || profile.ID != id {
				profile = user{ID: id}
			}
			name = profile.visualName()
			names[id] = name
		}
		displayNames = append(displayNames, name)
	}
	slices.Sort(displayNames)
	return strings.Join(displayNames, ", "), nil
}

func (u user) visualName() string {
	if name := strings.TrimSpace(strings.TrimSpace(u.FirstName) + " " + strings.TrimSpace(u.LastName)); name != "" {
		return name
	}
	if u.Username != "" {
		return u.Username
	}
	return u.ID
}
