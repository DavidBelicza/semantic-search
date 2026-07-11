// Package model holds the embedding model definitions: each model's id, vector size, and the
// prompt templates it needs to phrase a document chunk and a query. Every type here implements
// strategy.EmbeddingModel and does no I/O — transport is the sibling client package's job, so a
// model composes with any client. GemmaModel and the other named models carry fixed templates;
// GeneralModel is template-free for models that need none.
package model
