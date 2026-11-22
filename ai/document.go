/*
Package ai provides document representation, recursive text chunking, and sanitization.
*/
package ai

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Document represents an ingestible text chunk with its vector and metadata.
type Document struct {
	ID        any            `json:"id"`
	Content   string         `json:"content"`
	Embedding Vector         `json:"embedding,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// DocumentChunker defines the contract for splitting raw text into embeddable chunks.
type DocumentChunker interface {
	Chunk(text string) []string
}

// RecursiveChunker splits text hierarchically using natural boundary separators
// (paragraphs, line breaks, sentences, words, characters) while enforcing a maximum
// chunk size and an overlapping sliding window to preserve semantic continuity.
type RecursiveChunker struct {
	ChunkSize    int
	ChunkOverlap int
	Separators   []string
}

// NewRecursiveChunker constructs a RecursiveChunker with production-grade defaults:
// 1000 character chunks, 100 character overlap, splitting on natural language boundaries.
func NewRecursiveChunker(chunkSize, chunkOverlap int) *RecursiveChunker {
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	if chunkOverlap < 0 || chunkOverlap >= chunkSize {
		chunkOverlap = chunkSize / 10
	}
	return &RecursiveChunker{
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
		Separators:   []string{"\n\n", "\n", ". ", "? ", "! ", " ", ""},
	}
}

// Chunk splits text into a slice of string chunks obeying ChunkSize and ChunkOverlap.
func (c *RecursiveChunker) Chunk(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if utf8.RuneCountInString(text) <= c.ChunkSize {
		return []string{text}
	}
	return c.splitText(text, c.Separators)
}

func (c *RecursiveChunker) splitText(text string, separators []string) []string {
	var finalChunks []string
	separator := ""
	var nextSeparators []string

	for i, sep := range separators {
		if sep == "" || strings.Contains(text, sep) {
			separator = sep
			nextSeparators = separators[i+1:]
			break
		}
	}

	var splits []string
	if separator != "" {
		splits = strings.Split(text, separator)
	} else {
		// Individual runes if no separator matches
		runes := []rune(text)
		splits = make([]string, len(runes))
		for i, r := range runes {
			splits[i] = string(r)
		}
	}

	var currentDoc []string
	totalLen := 0

	for _, piece := range splits {
		pieceLen := utf8.RuneCountInString(piece)
		sepLen := 0
		if len(currentDoc) > 0 {
			sepLen = utf8.RuneCountInString(separator)
		}

		if totalLen+pieceLen+sepLen > c.ChunkSize && len(currentDoc) > 0 {
			chunk := strings.Join(currentDoc, separator)
			if utf8.RuneCountInString(chunk) > c.ChunkSize && len(nextSeparators) > 0 {
				subChunks := c.splitText(chunk, nextSeparators)
				finalChunks = append(finalChunks, subChunks...)
			} else {
				finalChunks = append(finalChunks, chunk)
			}

			// Retain overlap for the next window
			for totalLen > c.ChunkOverlap && len(currentDoc) > 0 {
				firstLen := utf8.RuneCountInString(currentDoc[0])
				if len(currentDoc) > 1 {
					firstLen += utf8.RuneCountInString(separator)
				}
				totalLen -= firstLen
				currentDoc = currentDoc[1:]
			}
		}

		currentDoc = append(currentDoc, piece)
		if len(currentDoc) == 1 {
			totalLen = pieceLen
		} else {
			totalLen += pieceLen + utf8.RuneCountInString(separator)
		}
	}

	if len(currentDoc) > 0 {
		chunk := strings.Join(currentDoc, separator)
		if utf8.RuneCountInString(chunk) > c.ChunkSize && len(nextSeparators) > 0 {
			subChunks := c.splitText(chunk, nextSeparators)
			finalChunks = append(finalChunks, subChunks...)
		} else {
			finalChunks = append(finalChunks, chunk)
		}
	}

	return finalChunks
}

var htmlTagRegex = regexp.MustCompile(`<[^>]*>`)
var multiWhitespaceRegex = regexp.MustCompile(`[ \t\r\f\v]+`)
var multiNewlineRegex = regexp.MustCompile(`\n{3,}`)

// SanitizeText strips basic HTML markup and collapses irregular whitespace,
// ensuring vector embeddings are computed on clean textual semantics.
func SanitizeText(raw string) string {
	clean := htmlTagRegex.ReplaceAllString(raw, " ")
	clean = multiWhitespaceRegex.ReplaceAllString(clean, " ")
	clean = strings.ReplaceAll(clean, " \n", "\n")
	clean = strings.ReplaceAll(clean, "\n ", "\n")
	clean = multiNewlineRegex.ReplaceAllString(clean, "\n\n")
	return strings.TrimSpace(clean)
}

// EstimateTokens provides a zero-dependency heuristic token count estimate
// based on standard OpenAI/Anthropic averages (~4 characters per token).
func EstimateTokens(text string) int {
	charCount := utf8.RuneCountInString(text)
	if charCount == 0 {
		return 0
	}
	tokens := charCount / 4
	if tokens == 0 {
		return 1
	}
	return tokens
}
