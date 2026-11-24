package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type apiClient struct {
	base          string
	host          string
	token         string
	eachMsgSuffix string
	http          *http.Client
}

type apiStatusError int

func (status apiStatusError) Error() string {
	return fmt.Sprintf("Mattermost returned HTTP %d", status)
}

func loadClient(path string) (*apiClient, error) {
	if path == "" {
		path = os.Getenv("MM_CLI_SETTINGS_PATH")
	}
	if path == "" {
		return nil, errors.New("specify --cfg PATH or set MM_CLI_SETTINGS_PATH")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read settings file: %w", err)
	}
	var settings struct {
		URL           string
		Token         string
		EachMsgSuffix string `json:"EACH_MSG_SUFFIX"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return nil, errors.New("invalid settings JSON: expected string URL and TOKEN fields and an optional string EACH_MSG_SUFFIX")
	}
	if strings.TrimSpace(settings.URL) == "" {
		return nil, errors.New("settings URL is required")
	}
	token := strings.TrimSpace(settings.Token)
	if token == "" {
		return nil, errors.New("settings TOKEN is required")
	}
	if strings.ContainsAny(token, " \t\r\n") {
		return nil, errors.New("settings TOKEN contains whitespace")
	}
	base, err := url.Parse(strings.TrimSpace(settings.URL))
	if err != nil || base.Hostname() == "" || (base.Scheme != "http" && base.Scheme != "https") ||
		base.User != nil || base.ForceQuery || base.RawQuery != "" || base.Fragment != "" || base.Opaque != "" {
		return nil, errors.New("settings URL must be an HTTP or HTTPS server base address without credentials, query or fragment")
	}
	base.Host = strings.ToLower(base.Host)
	return &apiClient{
		base:          strings.TrimRight(base.String(), "/"),
		host:          base.Hostname(),
		token:         token,
		eachMsgSuffix: settings.EachMsgSuffix,
		http: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (c *apiClient) request(method, path string, query url.Values, body, result any) error {
	endpoint := c.base + "/api/v4" + path
	if len(query) != 0 {
		endpoint += "?" + query.Encode()
	}
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, endpoint, payload)
	if err != nil {
		return errors.New("could not construct Mattermost request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) && urlErr.Timeout() {
			if method == http.MethodPost {
				return errors.New("Mattermost request timed out; send outcome is unknown")
			}
			return errors.New("Mattermost request timed out")
		}
		if method == http.MethodPost {
			return errors.New("Mattermost connection failed; send outcome is unknown")
		}
		return errors.New("Mattermost connection failed")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return apiStatusError(res.StatusCode)
	}
	if result != nil {
		if err := json.NewDecoder(res.Body).Decode(result); err != nil {
			if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
				return errors.New("Mattermost request timed out")
			}
			return errors.New("Mattermost returned an invalid JSON response")
		}
	} else {
		// A successful status confirms the send. Drain the body for connection reuse.
		_, _ = io.Copy(io.Discard, res.Body)
	}
	return nil
}

type user struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

func (c *apiClient) currentUser() (user, error) {
	var me user
	if err := c.request(http.MethodGet, "/users/me", nil, nil, &me); err != nil {
		return me, fmt.Errorf("get current user: %w", err)
	}
	if me.ID == "" {
		return me, errors.New("current user response is missing the ID")
	}
	return me, nil
}

func pathID(id string) string {
	if id == "." || id == ".." {
		return strings.ReplaceAll(id, ".", "%2E")
	}
	return url.PathEscape(id)
}
