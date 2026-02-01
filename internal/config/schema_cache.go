package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CachedSchema represents a cached tool schema
type CachedSchema struct {
	Command     string         `json:"command"`
	Schema      map[string]any `json:"schema"`
	HelpText    string         `json:"help_text"`
	GeneratedAt time.Time      `json:"generated_at"`
	Version     string         `json:"version,omitempty"` // Optional: command version
}

// SchemaCache manages cached tool schemas
type SchemaCache struct {
	cacheDir string
	mu       sync.RWMutex
}

// NewSchemaCache creates a new schema cache
func NewSchemaCache() (*SchemaCache, error) {
	cacheDir, err := SchemaCacheDir()
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(cacheDir, 0750); err != nil {
		return nil, err
	}

	return &SchemaCache{
		cacheDir: cacheDir,
	}, nil
}

// SchemaCacheDir returns the path to ~/.craby/cache/schemas/
func SchemaCacheDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cache", "schemas"), nil
}

// Get retrieves a cached schema if it exists and is not expired
func (c *SchemaCache) Get(command string) (*CachedSchema, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	path := c.schemaPath(command)
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is from user's config dir
	if err != nil {
		return nil, false
	}

	var schema CachedSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, false
	}

	// Check if cache is expired (default: 7 days)
	if time.Since(schema.GeneratedAt) > 7*24*time.Hour {
		return nil, false
	}

	return &schema, true
}

// Set stores a schema in the cache
func (c *SchemaCache) Set(schema *CachedSchema) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	schema.GeneratedAt = time.Now()

	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return err
	}

	path := c.schemaPath(schema.Command)
	//nolint:gosec // G306: cache files in user's config dir
	return os.WriteFile(path, data, 0640)
}

// Delete removes a cached schema
func (c *SchemaCache) Delete(command string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	path := c.schemaPath(command)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// List returns all cached command names
func (c *SchemaCache) List() ([]string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entries, err := os.ReadDir(c.cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var commands []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			cmd := entry.Name()[:len(entry.Name())-5] // Remove .json
			commands = append(commands, cmd)
		}
	}

	return commands, nil
}

// Clear removes all cached schemas
func (c *SchemaCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries, err := os.ReadDir(c.cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			_ = os.Remove(filepath.Join(c.cacheDir, entry.Name()))
		}
	}

	return nil
}

func (c *SchemaCache) schemaPath(command string) string {
	// Sanitize command name for filename
	safe := sanitizeFilename(command)
	return filepath.Join(c.cacheDir, safe+".json")
}

// sanitizeFilename removes/replaces characters unsafe for filenames
func sanitizeFilename(name string) string {
	var result []byte
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			result = append(result, c)
		} else {
			result = append(result, '_')
		}
	}
	return string(result)
}
