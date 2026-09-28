package library

import (
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

type Song struct {
	Singer   string `json:"singer"`
	Song     string `json:"song"`
	Language string `json:"language"`
	Style    string `json:"style"`
	Path     string `json:"path"`
	Name     string `json:"name"`
}

type ScanResult struct {
	Added            int      `json:"added"`
	SkippedInvalid   int      `json:"skippedInvalid"`
	SkippedDuplicate int      `json:"skippedDuplicate"`
	Invalid          []string `json:"invalid,omitempty"`
}

type Catalog struct {
	db *sql.DB
	mu sync.Mutex
}

func Open(dbPath string) (*Catalog, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS songs (
			id INTEGER PRIMARY KEY,
			singer TEXT NOT NULL,
			song TEXT NOT NULL,
			language TEXT NOT NULL,
			style TEXT NOT NULL,
			path TEXT NOT NULL UNIQUE
		)`); err != nil {
		db.Close()
		return nil, err
	}
	return &Catalog{db: db}, nil
}

func (c *Catalog) Close() error {
	return c.db.Close()
}

// ParseFilename accepts "<Singer>-<Song>-<Language>-<Style>.mkv" with exactly four non-empty parts.
func ParseFilename(name string) (singer, song, language, style string, ok bool) {
	ext := filepath.Ext(name)
	if !strings.EqualFold(ext, ".mkv") {
		return "", "", "", "", false
	}
	stem := strings.TrimSuffix(name, ext)
	parts := strings.Split(stem, "-")
	if len(parts) != 4 {
		return "", "", "", "", false
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
		if parts[i] == "" {
			return "", "", "", "", false
		}
	}
	return parts[0], parts[1], parts[2], parts[3], true
}

func (s Song) displayName() string {
	return s.Singer + " - " + s.Song
}

func (c *Catalog) Search(query string) ([]Song, error) {
	query = strings.TrimSpace(query)
	var (
		rows *sql.Rows
		err  error
	)
	if query == "" {
		rows, err = c.db.Query(`SELECT singer, song, language, style, path FROM songs ORDER BY singer, song`)
	} else {
		pat := likeContains(query)
		rows, err = c.db.Query(`
			SELECT singer, song, language, style, path
			FROM songs
			WHERE singer LIKE ? ESCAPE '\' OR song LIKE ? ESCAPE '\'
			ORDER BY singer, song`, pat, pat)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Song{}
	for rows.Next() {
		var s Song
		if err := rows.Scan(&s.Singer, &s.Song, &s.Language, &s.Style, &s.Path); err != nil {
			return nil, err
		}
		s.Name = s.displayName()
		out = append(out, s)
	}
	return out, rows.Err()
}

func (c *Catalog) ByPath(rel string) (Song, bool) {
	var s Song
	err := c.db.QueryRow(`
		SELECT singer, song, language, style, path
		FROM songs WHERE path = ?`, rel).Scan(&s.Singer, &s.Song, &s.Language, &s.Style, &s.Path)
	if err != nil {
		return Song{}, false
	}
	s.Name = s.displayName()
	return s, true
}

// Scan walks every folder under videoDir, inserts valid new MKV files, and leaves existing rows in place.
func (c *Catalog) Scan(videoDir string) (ScanResult, error) {
	var found []Song
	var result ScanResult
	err := filepath.WalkDir(videoDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".mkv") {
			return nil
		}
		singer, song, language, style, ok := ParseFilename(d.Name())
		if !ok {
			result.SkippedInvalid++
			if len(result.Invalid) < 50 {
				result.Invalid = append(result.Invalid, d.Name())
			}
			return nil
		}
		rel, err := filepath.Rel(videoDir, p)
		if err != nil {
			return err
		}
		found = append(found, Song{
			Singer:   singer,
			Song:     song,
			Language: language,
			Style:    style,
			Path:     filepath.ToSlash(rel),
		})
		return nil
	})
	if err != nil {
		return ScanResult{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	tx, err := c.db.Begin()
	if err != nil {
		return ScanResult{}, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO songs (singer, song, language, style, path)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(path) DO NOTHING`)
	if err != nil {
		return ScanResult{}, err
	}
	defer stmt.Close()

	for _, s := range found {
		res, err := stmt.Exec(s.Singer, s.Song, s.Language, s.Style, s.Path)
		if err != nil {
			return ScanResult{}, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return ScanResult{}, err
		}
		if n == 0 {
			result.SkippedDuplicate++
			continue
		}
		result.Added++
	}
	if err := tx.Commit(); err != nil {
		return ScanResult{}, err
	}
	return result, nil
}

func (c *Catalog) Clear() (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	res, err := c.db.Exec(`DELETE FROM songs`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func likeContains(q string) string {
	var b strings.Builder
	b.WriteByte('%')
	for _, r := range q {
		switch r {
		case '%', '_', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('%')
	return b.String()
}
