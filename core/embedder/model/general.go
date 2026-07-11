package model

import "github.com/davidbelicza/semantic-search/core/storage"

// GeneralModel is a template-free model: it embeds the raw chunk and query text, with no
// model-specific prompt formatting. Its id and vector size are supplied by the caller, so
// switching to a different OpenAI-standard model — or a different vector size of the same one —
// needs no new type, just a different constructor call.
type GeneralModel struct {
	name       string
	dimensions int
}

// NewGeneralModel builds a template-free model with the given model id and vector size.
func NewGeneralModel(name string, dimensions int) GeneralModel {
	return GeneralModel{name: name, dimensions: dimensions}
}

func (m GeneralModel) Name() string { return m.name }

func (m GeneralModel) Dimensions() int { return m.dimensions }

// BuildData embeds the chunk's text as-is, without the title or any prompt template.
func (GeneralModel) BuildData(chunk storage.Chunk) string {
	return chunk.Text
}

// BuildQuery embeds the query unchanged. A template-free model has no task template, so a
// non-empty task type is rejected rather than silently ignored.
func (m GeneralModel) BuildQuery(query, taskType string) (string, error) {
	if taskType != "" {
		return "", unsupportedTaskType(m.name)
	}

	return query, nil
}
