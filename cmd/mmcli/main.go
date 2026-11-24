package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
)

const usage = `Usage:
  mmcli send --channel LIST --text LIST
  mmcli read [--limit N] [--channel LIST] [--cursor TOKEN]
  mmcli mark-read --msg-id LIST
  mmcli list-channels [--force] [find PATTERN]
`

type sendResult struct {
	SentCount   int    `json:"sent-count"`
	ChannelID   string `json:"channel-id,omitempty"`
	MessageItem int    `json:"message-item,omitempty"`
}

type markReadResult struct {
	ProcessedIDs []string `json:"processed-ids"`
	MessageID    string   `json:"msg-id,omitempty"`
}

type commandError struct {
	Message string `json:"error"`
	*sendResult
	*markReadResult
}

func (e *commandError) Error() string {
	return e.Message
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		var failure *commandError
		if !errors.As(err, &failure) {
			failure = &commandError{Message: err.Error()}
		}
		_ = writeJSON(os.Stderr, failure)
		os.Exit(1)
	}
}

func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("expected send, read, mark-read or list-channels (see --help)")
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var channels, messages, ids, cursorToken string
	var limit int
	var force bool
	switch command {
	case "--help", "-h", "help":
		_, err := io.WriteString(out, usage)
		return err
	case "send":
		flags.StringVar(&channels, "channel", "", "channel IDs")
		flags.StringVar(&messages, "text", "", "messages")
	case "read":
		flags.StringVar(&channels, "channel", "", "channel IDs")
		flags.StringVar(&cursorToken, "cursor", "", "pagination cursor")
		flags.IntVar(&limit, "limit", 20, "maximum number of messages")
	case "mark-read":
		flags.StringVar(&ids, "msg-id", "", "message IDs")
	case "list-channels":
		flags.BoolVar(&force, "force", false, "refresh the channel cache from Mattermost")
	default:
		return fmt.Errorf("unknown command %q (see --help)", command)
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, err := io.WriteString(out, usage)
			return err
		}
		return err
	}
	var find *regexp.Regexp
	if command == "list-channels" && flags.NArg() != 0 {
		if flags.NArg() != 2 || flags.Arg(0) != "find" {
			return errors.New("expected list-channels [--force] [find PATTERN]")
		}
		pattern := strings.NewReplacer("%", ".*", "_", ".").Replace(regexp.QuoteMeta(flags.Arg(1)))
		var err error
		// An unanchored match supplies the implicit leading and trailing SQL %.
		find, err = regexp.Compile("(?is)" + pattern)
		if err != nil {
			return fmt.Errorf("invalid find pattern: %w", err)
		}
	} else if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	provided := make(map[string]bool)
	flags.Visit(func(f *flag.Flag) { provided[f.Name] = true })

	var channelIDs, texts, messageIDs []string
	var err error
	if command == "send" || provided["channel"] {
		channelIDs, err = listOption("channel", channels, provided["channel"])
		if err != nil {
			return err
		}
	}
	switch command {
	case "send":
		texts, err = listOption("text", messages, provided["text"])
	case "mark-read":
		messageIDs, err = listOption("msg-id", ids, provided["msg-id"])
	case "read":
		if limit < 1 {
			return errors.New("--limit must be positive")
		}
		if provided["cursor"] && cursorToken == "" {
			return errors.New("--cursor must be nonempty")
		}
	}
	if err != nil {
		return err
	}
	client, err := loadClient()
	if err != nil {
		return err
	}
	if command == "send" {
		return send(client, channelIDs, texts, out)
	}
	if command == "list-channels" {
		result, err := client.channelList(force)
		if err != nil {
			return err
		}
		if find != nil {
			result.Channels = slices.DeleteFunc(result.Channels, func(ch listedChannel) bool {
				return !find.MatchString(ch.VisualName)
			})
		}
		return writeJSON(out, result)
	}
	user, err := client.currentUser()
	if err != nil {
		return err
	}
	scope := digest(client.base, user.ID)
	markers, err := markerDirectory(scope)
	if err != nil {
		return err
	}
	if command == "mark-read" {
		return markRead(client, markers, messageIDs, out)
	}
	return read(client, user, markers, scope, channelIDs, limit, cursorToken, out)
}

func send(client *apiClient, channels, texts []string, out io.Writer) error {
	var result sendResult
	for index, message := range texts {
		for _, channel := range channels {
			body := map[string]string{"channel_id": channel, "message": message}
			if err := client.request("POST", "/posts", nil, body, nil); err != nil {
				result.ChannelID = channel
				result.MessageItem = index + 1
				return &commandError{Message: err.Error(), sendResult: &result}
			}
			result.SentCount++
		}
	}
	return writeJSON(out, result)
}
