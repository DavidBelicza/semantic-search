package model

import (
	"strings"

	"github.com/davidbelicza/semantic-search/core/storage"
)

// titledText prefixes a chunk's heading path to its body so the title is embedded alongside
// the text. A chunk's title carries the headings it sits under, which is often the only place
// a section's subject appears; leaving it out makes those chunks retrievable by their body
// wording alone. Models with their own documented title template (EmbeddingGemma) format the
// title themselves and do not use this.
func titledText(chunk storage.Chunk) string {
	title := strings.TrimSpace(chunk.Title)
	if title == "" {
		return chunk.Text
	}

	return title + "\n" + chunk.Text
}
