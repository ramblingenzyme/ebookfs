package kosync

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// MappingFile manages the document_id → book_id mapping.
// This is a derived index that can be rebuilt from sidecars if lost.
type MappingFile struct {
	path string
	mu   sync.RWMutex
	data map[string]int64 // document_id → book_id
}

// NewEmptyMapping creates an empty mapping file at the specified directory.
func NewEmptyMapping(kosyncDir string) *MappingFile {
	path := filepath.Join(kosyncDir, "mapping.json")
	// Ensure directory exists
	if err := os.MkdirAll(kosyncDir, 0755); err != nil {
		slog.Error("failed to create kosync directory", "error", err)
	}
	return &MappingFile{
		path: path,
		data: make(map[string]int64),
	}
}

// LoadMapping loads the mapping file from disk.
// If the file doesn't exist, it creates an empty mapping.
func LoadMapping(kosyncDir string) (*MappingFile, error) {
	path := filepath.Join(kosyncDir, "mapping.json")
	m := &MappingFile{
		path: path,
		data: make(map[string]int64),
	}

	// Ensure directory exists
	if err := os.MkdirAll(kosyncDir, 0755); err != nil {
		return nil, fmt.Errorf("creating kosync directory: %w", err)
	}

	// Try to load existing mapping
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return nil, fmt.Errorf("opening mapping: %w", err)
	}
	defer f.Close()

	if err := json.NewDecoder(f).Decode(&m.data); err != nil {
		return nil, fmt.Errorf("decoding mapping: %w", err)
	}

	return m, nil
}

// Get returns the book_id for a document_id, or 0 if not found.
func (m *MappingFile) Get(documentID string) (int64, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	bookID, ok := m.data[documentID]
	return bookID, ok
}

// Set adds or updates a mapping from document_id to book_id and persists to disk.
func (m *MappingFile) Set(documentID string, bookID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[documentID] = bookID
	if err := m.save(); err != nil {
		slog.Error("kosync: failed to save mapping after set", "error", err)
	}
}

// Rebuild reconstructs the mapping from all book sidecars.
// It reads the kosync.json sidecar from each book in the library and
// rebuilds the document_id → book_id mapping atomically.
func (m *MappingFile) Rebuild(lib *library.Library) error {
	// Get all books using empty query (matches everything)
	books, err := lib.Search(library.Query{})
	if err != nil {
		return fmt.Errorf("searching library: %w", err)
	}

	// Build new map without holding the lock
	newData := make(map[string]int64)
	booksScanned := 0
	booksWithSidecar := 0

	for _, book := range books {
		booksScanned++

		// Read the kosync.json sidecar
		sidecarData, err := lib.ReadSidecar(book.ID(), "kosync.json")
		if err != nil {
			continue
		}

		var sidecar struct {
			DocumentIDs []string `json:"document_ids"`
		}
		if err := json.Unmarshal(sidecarData, &sidecar); err != nil {
			slog.Warn("kosync: corrupt sidecar during rebuild", "book_id", book.ID(), "error", err)
			continue
		}

		// Add all document IDs to the new map
		for _, docID := range sidecar.DocumentIDs {
			if docID != "" {
				newData[docID] = book.ID()
			}
		}
		booksWithSidecar++
	}

	// Atomic swap and persist: acquire write lock, replace the map, and save
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = newData
	if err := m.save(); err != nil {
		return fmt.Errorf("saving rebuilt mapping: %w", err)
	}

	return nil
}

// Delete removes a document_id from the mapping and persists to disk.
func (m *MappingFile) Delete(documentID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, documentID)
	if err := m.save(); err != nil {
		slog.Error("kosync: failed to save mapping after delete", "error", err)
	}
}

// save writes the mapping to disk atomically.
// Caller must hold the write lock.
func (m *MappingFile) save() error {

	// Write to temp file first
	tmpPath := m.path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("creating temp mapping: %w", err)
	}

	if err := json.NewEncoder(f).Encode(m.data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("encoding mapping: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing temp mapping: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, m.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming mapping: %w", err)
	}

	return nil
}

// Snapshot returns a copy of the current mapping for testing.
func (m *MappingFile) Snapshot() map[string]int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	copy := make(map[string]int64, len(m.data))
	for k, v := range m.data {
		copy[k] = v
	}
	return copy
}
