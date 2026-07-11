// Package client holds the embedding transport clients: the code that talks to an embedding
// server over the wire (protocol, auth, retries). It implements strategy.AiClient and knows
// nothing model-specific — the prompt templates live in the sibling model package. OpenAIClient
// speaks the OpenAI-compatible /v1/embeddings protocol; further standards can be added alongside
// it without touching the models.
package client
