package main

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

func listOption(name, value string, provided bool) ([]string, error) {
	if !provided {
		return nil, fmt.Errorf("--%s is required", name)
	}
	items, err := parseList(value)
	if err != nil {
		return nil, fmt.Errorf("--%s: %w", name, err)
	}
	return items, nil
}

func parseList(input string) ([]string, error) {
	var items []string
	for {
		input = strings.TrimLeftFunc(input, unicode.IsSpace)
		var item string
		if strings.HasPrefix(input, "\"") {
			var err error
			item, input, err = quotedItem(input[1:])
			if err != nil {
				return nil, err
			}
			input = strings.TrimLeftFunc(input, unicode.IsSpace)
		} else {
			raw, _, _ := strings.Cut(input, ",")
			if strings.ContainsRune(raw, '"') {
				return nil, errors.New("unexpected double quote in unquoted item")
			}
			item = strings.TrimSpace(raw)
			input = input[len(raw):]
		}
		if strings.TrimSpace(item) == "" {
			return nil, fmt.Errorf("item %d is empty or contains only whitespace", len(items)+1)
		}
		items = append(items, item)
		if input == "" {
			return items, nil
		}
		if input[0] != ',' {
			return nil, errors.New("expected comma after quoted item")
		}
		input = input[1:]
	}
}

func quotedItem(input string) (string, string, error) {
	var item strings.Builder
	for {
		before, after, found := strings.Cut(input, "\"")
		if !found {
			return "", "", errors.New("unclosed double quote")
		}
		item.WriteString(before)
		if !strings.HasPrefix(after, "\"") {
			return item.String(), after, nil
		}
		item.WriteByte('"')
		input = after[1:]
	}
}
