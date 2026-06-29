package parser

import "context"

type MarkdownParser struct{}

func (p MarkdownParser) Parse(ctx context.Context, text string) (string, error) {
	return text, nil
}
