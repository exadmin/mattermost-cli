# mmcli Specification

`mmcli` is a Go command-line application that uses the Mattermost REST API v4.

## Settings

The `MM_CLI_SETTINGS_PATH` environment variable specifies the path to the JSON file from which `mmcli` loads all its settings. The MVP requires two string fields.

```json
{
  "URL": "https://mattermost.example.com",
  "TOKEN": "your-personal-access-token"
}
```

`URL` is the base address of the Mattermost server. `TOKEN` is the token of the user on whose behalf commands run. Future settings go in the same file.

If the variable is unset, the file is inaccessible, the JSON is invalid, or a required value is missing, the command exits with a nonzero status and reports the cause without printing the token.

## Command output

Format JSON results and errors across multiple lines with two spaces per indentation level.

Existing and future commands return one JSON object on standard output and exit with status 0 on success. On error, they return one JSON object on standard error, leave standard output empty, and exit with a nonzero status. Every error object includes an `error` string. Commands that can partially succeed include progress in the error object when an operation fails. Use hyphen-separated names for multiword JSON fields and empty arrays for empty lists. Help output is plain text.

## `mmcli send`

```text
mmcli send --channel 1,2,3,4,other --text "aaa","bbb"
```

`--channel` and `--text` are required. Each accepts a comma-separated list. `--channel` accepts channel IDs, including IDs of existing direct and group chats.

The application sends `aaa` to each listed channel in order, then sends `bbb` to the same channels in the same order. This example makes ten separate sends. If a request fails, the CLI identifies the channel and message list item involved and exits with a nonzero status. Messages already sent remain in Mattermost. If a request times out and its outcome is unknown, the CLI does not retry it automatically.

The result contains `sent-count`, the number of confirmed sends. If a send fails, the error object includes `sent-count` for the sends confirmed before the failure, plus the failed request's `channel-id` and one-based `message-item` index.

## `mmcli read`

```text
mmcli read --limit 20 --channel 1,2,3
mmcli read --limit 20
mmcli read --limit 20 --channel 1,2,3 --cursor TOKEN
```

The command lists unread messages from the selected channels, newest first. Message IDs break timestamp ties. `--limit` caps the total number of messages per page across all selected channels and defaults to 20. `--channel` restricts the results to the listed channel IDs. If omitted, the command covers all channels and chats the user belongs to.

The result contains a `messages` array. Each message contains the following fields.

| Field | Value |
| --- | --- |
| `msg-id` | Mattermost message ID |
| `time-stamp` | Creation time in UTC, formatted as RFC 3339 |
| `channel-id` | Channel or chat ID |
| `sender` | Sender's username, or their user ID if the username is unavailable |
| `text` | Message text |

When another page is available, the command returns `next-cursor`. Pass it as `--cursor` with the same channel filter. Request the first page again to see new messages. Read status can change between requests, so subsequent pages reflect the current unread state. The `read` command preserves read status in both Mattermost and `mmcli`.

## `mmcli mark-read`

```text
mmcli mark-read --msg-id 1,2,3
```

`--msg-id` is required and accepts a list of message IDs. The command verifies that the current user can access each message and saves a read marker for each specified ID. Marking the same ID again has the same effect. If an error occurs, the CLI reports which IDs it processed successfully.

The result contains a `processed-ids` array in input order. If processing a message fails, the error object includes the `processed-ids` confirmed before the failure and the failed `msg-id`.

The Mattermost API marks entire channels as read. To support marking individual messages, `mmcli` persists read markers locally across runs, separately for each server and user. `mmcli read` excludes locally marked messages from its unread results. Their read status in the Mattermost web app and other clients stays the same. [Mattermost Channels API](https://docs.mattermost.com/api/reference/channels)

## `mmcli list-channels`

```text
mmcli list-channels
```

The command lists active public channels visible in the user's teams, private
channels the user belongs to, and existing direct and group chats. Public
channels may require joining before sending. The command uses real Mattermost
channel IDs and discovers users through existing direct chats. It performs
read-only requests.

The result contains a `channels` array sorted by channel ID. Each object has
`channel-id`, `visibility`, and `visual-name` fields. Visibility is `public` for
public channels and `private` for private channels and chats. The visual name
is the channel's display name, the direct chat participant's name, or the group chat's
display name. An unnamed group chat uses the other participants' names.
Participant names use the available first and last name, falling back to the
username or user ID. A chat with oneself uses the current user's name.

## Lists and quoting

`--channel`, `--text`, and `--msg-id` share the same list syntax. A comma outside double quotes separates items. Inside double quotes, commas and spaces are preserved, and two consecutive double quotes represent a literal double quote. Unclosed quotes, empty items, and messages containing only whitespace are errors.

The CLI parses each list from one argument. The shell must pass the inner double quotes as part of that argument. For example, in a shell that preserves double quotes inside single quotes, `--text '"Hello, team","The build is ready"'` passes two messages. If the inner quotes are stripped, the comma after “Hello” separates list items.
