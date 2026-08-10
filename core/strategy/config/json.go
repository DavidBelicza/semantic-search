package config

import (
	"encoding/json"
	"strconv"
	"strings"
)

// parseJSON reads the token stream rather than unmarshalling into map[string]any, because Go
// randomizes map iteration and section order would then differ between runs.
func parseJSON(source string) (node, error) {
	decoder := json.NewDecoder(strings.NewReader(source))
	decoder.UseNumber()

	token, err := decoder.Token()
	if err != nil {
		return node{}, err
	}

	return jsonValue(decoder, token, "")
}

func jsonValue(decoder *json.Decoder, token json.Token, key string) (node, error) {
	delim, ok := token.(json.Delim)
	if !ok {
		return node{Key: key, Value: jsonScalar(token)}, nil
	}

	if delim == '{' {
		return jsonObject(decoder, key)
	}

	return jsonArray(decoder, key)
}

func jsonObject(decoder *json.Decoder, key string) (node, error) {
	result := node{Key: key}

	for {
		nameToken, err := decoder.Token()
		if err != nil {
			return node{}, err
		}
		if isClosingDelim(nameToken) {
			return result, nil
		}

		child, err := jsonMember(decoder, nameToken)
		if err != nil {
			return node{}, err
		}

		result.Children = append(result.Children, child)
	}
}

func jsonMember(decoder *json.Decoder, nameToken json.Token) (node, error) {
	name, _ := nameToken.(string)

	valueToken, err := decoder.Token()
	if err != nil {
		return node{}, err
	}

	return jsonValue(decoder, valueToken, name)
}

func jsonArray(decoder *json.Decoder, key string) (node, error) {
	result := node{Key: key}

	for {
		token, err := decoder.Token()
		if err != nil {
			return node{}, err
		}
		if isClosingDelim(token) {
			return result, nil
		}

		child, err := jsonValue(decoder, token, "")
		if err != nil {
			return node{}, err
		}

		result.Children = append(result.Children, child)
	}
}

func isClosingDelim(token json.Token) bool {
	delim, ok := token.(json.Delim)

	return ok && (delim == '}' || delim == ']')
}

func jsonScalar(token json.Token) string {
	switch value := token.(type) {
	case nil:
		return "null"
	case string:
		return value
	case json.Number:
		return value.String()
	case bool:
		return strconv.FormatBool(value)
	default:
		return ""
	}
}
