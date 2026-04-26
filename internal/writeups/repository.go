package writeups

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type SearchResult struct {
	ID      int
	Title   string
	Path    string
	Snippet string
}

type ReadResult struct {
	Path       string
	StartLine  int
	EndLine    int
	TotalLines int
	Text       string
}

type Repository struct {
	dbPath string
	db     *sql.DB
}

func DefaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "writeups_index.db"
	}

	return filepath.Join(home, "writeups-mcp-opencode", "data", "writeups_index.db")
}

func DBPathFromEnv() string {
	if value := strings.TrimSpace(os.Getenv("WRITEUPS_DB")); value != "" {
		return value
	}

	return DefaultDBPath()
}

func Open(dbPath string) (*Repository, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Repository{dbPath: dbPath, db: db}, nil
}

func (r *Repository) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (r *Repository) Search(query string, limit int) ([]SearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("query is required")
	}

	if limit <= 0 {
		limit = 10
	}

	rows, err := r.db.Query(
		`SELECT d.rowid, d.title, d.path,
		 snippet(docs_fts, -1, ' ', ' ', ' ', 10) AS snippet
		 FROM docs d
		 JOIN docs_fts f ON d.rowid = f.rowid
		 WHERE docs_fts MATCH ?
		 LIMIT ?`,
		query,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]SearchResult, 0, limit)
	for rows.Next() {
		var item SearchResult
		if err := rows.Scan(&item.ID, &item.Title, &item.Path, &item.Snippet); err != nil {
			return nil, err
		}
		item.Snippet = strings.ReplaceAll(item.Snippet, "\n", " ")
		item.Snippet = strings.ReplaceAll(item.Snippet, "==", "")
		results = append(results, item)
	}

	return results, rows.Err()
}

func (r *Repository) ResolvePathByID(id int) (string, error) {
	if id <= 0 {
		return "", errors.New("id must be positive")
	}

	var filePath string
	err := r.db.QueryRow(`SELECT path FROM docs WHERE rowid = ?`, id).Scan(&filePath)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("writeup with id %d not found", id)
		}
		return "", err
	}

	return filePath, nil
}

func (r *Repository) Read(id int, directPath, linesArg string) (*ReadResult, error) {
	filePath := strings.TrimSpace(directPath)
	if id > 0 {
		resolved, err := r.ResolvePathByID(id)
		if err != nil {
			return nil, err
		}
		filePath = resolved
	}

	if filePath == "" {
		return nil, errors.New("need id or path")
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(content), "\n")
	start, end, err := parseLineRange(linesArg, len(lines))
	if err != nil {
		return nil, err
	}

	formatted := make([]string, 0, end-start+1)
	for idx, line := range lines[start-1 : end] {
		formatted = append(formatted, fmt.Sprintf("%4d| %s", start+idx, line))
	}

	text := fmt.Sprintf(
		"%s [lines %d-%d of %d]\n%s\n%s",
		filepath.Base(filePath),
		start,
		end,
		len(lines),
		strings.Repeat("-", 50),
		strings.Join(formatted, "\n"),
	)

	return &ReadResult{
		Path:       filePath,
		StartLine:  start,
		EndLine:    end,
		TotalLines: len(lines),
		Text:       text,
	}, nil
}

func parseLineRange(linesArg string, totalLines int) (int, int, error) {
	if totalLines <= 0 {
		return 1, 1, nil
	}

	value := strings.TrimSpace(linesArg)
	if value == "" {
		return 1, min(100, totalLines), nil
	}

	if strings.Contains(value, "-") && !strings.HasSuffix(value, "-") {
		parts := strings.SplitN(value, "-", 2)
		start, err := parsePositive(parts[0], 1)
		if err != nil {
			return 0, 0, err
		}
		end, err := parsePositive(parts[1], totalLines)
		if err != nil {
			return 0, 0, err
		}
		if start > end {
			return 0, 0, errors.New("invalid line range: start is greater than end")
		}
		return max(1, start), min(totalLines, end), nil
	}

	if strings.HasSuffix(value, "-") {
		start, err := parsePositive(strings.TrimSuffix(value, "-"), 1)
		if err != nil {
			return 0, 0, err
		}
		return max(1, start), totalLines, nil
	}

	line, err := parsePositive(value, 1)
	if err != nil {
		return 0, 0, err
	}

	return max(1, line-10), min(totalLines, line+10), nil
}

func parsePositive(raw string, fallback int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}

	var value int
	_, err := fmt.Sscanf(raw, "%d", &value)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid line value: %q", raw)
	}

	return value, nil
}

func FormatSearchResults(query string, results []SearchResult) string {
	if len(results) == 0 {
		return fmt.Sprintf("Not found: %s", query)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "=== Found %d ===\n\n", len(results))
	for _, item := range results {
		fmt.Fprintf(&b, "%d. %s\n", item.ID, item.Title)
		fmt.Fprintf(&b, "   %s\n", item.Path)
		if strings.TrimSpace(item.Snippet) != "" {
			fmt.Fprintf(&b, "   %s...\n", item.Snippet)
		}
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

func HelpText(tool string) string {
	switch tool {
	case "search":
		return `search_writeups - search the writeups knowledge base
==========================================================
Contains: CTF writeups, HackTricks, exploitation notes, vulnerability explanations,
cheat sheets, and other security documentation.

PARAMETERS:
- query (string, required): search phrase
- limit (number, optional): maximum results, default 10

EXAMPLES:
 search_writeups({query: "SQL injection", limit: 5})
 search_writeups({query: "privilege escalation windows"})
 search_writeups({query: "CVE-2024-1709"})

RESULT:
Returns numbered results. Use read_writeup with the returned id.`
	case "read":
		return `read_writeup - read a writeup file
=====================================
Reads a full file or a selected line range by search result id or direct path.

PARAMETERS:
- id (number, optional): id from search results
- path (string, optional): direct file path
- lines (string, optional):
  * "100-150" - exact range
  * "50" - around line 50
  * "100-" - from line 100 to end
  * empty - first 100 lines

EXAMPLES:
 read_writeup({id: 1})
 read_writeup({id: 1, lines: "1-50"})
 read_writeup({path: "/path/to/file.md", lines: "100"})`
	case "all", "":
		return HelpText("search") + "\n\n" + HelpText("read")
	default:
		return fmt.Sprintf("Unknown tool: %s. Use: search, read, or all", tool)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
