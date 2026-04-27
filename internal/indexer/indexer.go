package indexer

import (
	"bytes"
	"context"
	"crypto/sha1"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

var defaultExtensions = map[string]struct{}{
	".md":   {},
	".txt":  {},
	".rst":  {},
	".html": {},
}

type Options struct {
	SourceDir    string
	DBPath       string
	MinChars     int
	Workers      int
	CommitEvery  int
	Extensions   []string
	PruneDeleted bool
}

type Stats struct {
	Candidates     int64
	Processed      int64
	Indexed        int64
	Updated        int64
	SkippedShort   int64
	SkippedDup     int64
	SkippedCached  int64
	DeletedMissing int64
	ReadErrors     int64
	StartedAt      time.Time
	FinishedAt     time.Time
}

type ProgressWriter func(stats Stats)

type fileMeta struct {
	ID      int64
	Path    string
	Hash    string
	Size    int64
	ModTime int64
	Title   string
}

type fileJob struct {
	Path string
	Info fs.FileInfo
	Meta fileMeta
}

type fileResult struct {
	Path     string
	Title    string
	Hash     string
	Content  string
	Size     int64
	ModTime  int64
	Existing fileMeta
	Skip     bool
	Reason   string
	Err      error
}

func Run(ctx context.Context, opts Options) (*Stats, error) {
	return RunWithProgress(ctx, opts, nil)
}

func RunWithProgress(ctx context.Context, opts Options, progress ProgressWriter) (*Stats, error) {
	if err := validateOptions(&opts); err != nil {
		return nil, err
	}

	stats := &Stats{StartedAt: time.Now()}
	if progress != nil {
		defer progress(snapshotStats(stats))
		stop := startProgressReporter(ctx, stats, progress)
		defer stop()
	}

	db, err := openDB(opts.DBPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	if err := ensureSchema(db); err != nil {
		return nil, err
	}

	existing, err := loadExisting(db)
	if err != nil {
		return nil, err
	}

	paths, err := gatherPaths(opts.SourceDir, extensionSet(opts.Extensions), stats)
	if err != nil {
		return nil, err
	}

	results, err := processFiles(ctx, paths, existing, opts, stats)
	if err != nil {
		return nil, err
	}

	if err := writeResults(db, results, existing, opts, stats); err != nil {
		return nil, err
	}

	stats.FinishedAt = time.Now()
	return stats, nil
}

func startProgressReporter(ctx context.Context, stats *Stats, progress ProgressWriter) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				progress(snapshotStats(stats))
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}

func snapshotStats(stats *Stats) Stats {
	if stats == nil {
		return Stats{}
	}

	return Stats{
		Candidates:     atomic.LoadInt64(&stats.Candidates),
		Processed:      atomic.LoadInt64(&stats.Processed),
		Indexed:        atomic.LoadInt64(&stats.Indexed),
		Updated:        atomic.LoadInt64(&stats.Updated),
		SkippedShort:   atomic.LoadInt64(&stats.SkippedShort),
		SkippedDup:     atomic.LoadInt64(&stats.SkippedDup),
		SkippedCached:  atomic.LoadInt64(&stats.SkippedCached),
		DeletedMissing: atomic.LoadInt64(&stats.DeletedMissing),
		ReadErrors:     atomic.LoadInt64(&stats.ReadErrors),
		StartedAt:      stats.StartedAt,
		FinishedAt:     stats.FinishedAt,
	}
}

func validateOptions(opts *Options) error {
	if strings.TrimSpace(opts.SourceDir) == "" {
		return errors.New("source directory is required")
	}
	info, err := os.Stat(opts.SourceDir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("source path is not a directory: %s", opts.SourceDir)
	}
	if strings.TrimSpace(opts.DBPath) == "" {
		return errors.New("database path is required")
	}
	if opts.MinChars <= 0 {
		opts.MinChars = 300
	}
	if opts.Workers <= 0 {
		opts.Workers = max(2, runtime.NumCPU())
	}
	if opts.CommitEvery <= 0 {
		opts.CommitEvery = 200
	}
	if len(opts.Extensions) == 0 {
		for ext := range defaultExtensions {
			opts.Extensions = append(opts.Extensions, ext)
		}
		sort.Strings(opts.Extensions)
	}
	return nil
}

func openDB(dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA temp_store=MEMORY",
		"PRAGMA foreign_keys=ON",
	}
	for _, stmt := range pragmas {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return nil, err
		}
	}

	return db, db.Ping()
}

func ensureSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS docs (
			id INTEGER PRIMARY KEY,
			path TEXT UNIQUE,
			hash TEXT,
			title TEXT,
			size_bytes INTEGER DEFAULT 0,
			mod_time_ns INTEGER DEFAULT 0
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS docs_fts USING fts5(
			content,
			path UNINDEXED,
			title UNINDEXED,
			tokenize = "unicode61 remove_diacritics 1"
		)`,
		`CREATE INDEX IF NOT EXISTS idx_docs_path ON docs(path)`,
		`CREATE INDEX IF NOT EXISTS idx_docs_hash ON docs(hash)`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}

	columns := []string{
		`ALTER TABLE docs ADD COLUMN size_bytes INTEGER DEFAULT 0`,
		`ALTER TABLE docs ADD COLUMN mod_time_ns INTEGER DEFAULT 0`,
	}
	for _, stmt := range columns {
		_, _ = db.Exec(stmt)
	}

	return nil
}

func loadExisting(db *sql.DB) (map[string]fileMeta, error) {
	rows, err := db.Query(`SELECT id, path, hash, COALESCE(size_bytes, 0), COALESCE(mod_time_ns, 0), COALESCE(title, '') FROM docs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make(map[string]fileMeta)
	for rows.Next() {
		var item fileMeta
		if err := rows.Scan(&item.ID, &item.Path, &item.Hash, &item.Size, &item.ModTime, &item.Title); err != nil {
			return nil, err
		}
		items[item.Path] = item
	}
	return items, rows.Err()
}

func gatherPaths(root string, exts map[string]struct{}, stats *Stats) ([]string, error) {
	paths := make([]string, 0, 1024)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if _, ok := exts[strings.ToLower(filepath.Ext(path))]; !ok {
			return nil
		}
		paths = append(paths, path)
		atomic.AddInt64(&stats.Candidates, 1)
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func processFiles(ctx context.Context, paths []string, existing map[string]fileMeta, opts Options, stats *Stats) ([]fileResult, error) {
	jobs := make(chan fileJob, opts.Workers*2)
	resultsCh := make(chan fileResult, opts.Workers*2)

	var wg sync.WaitGroup
	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				resultsCh <- processOne(job, opts.MinChars)
			}
		}()
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	go func() {
		defer close(jobs)
		for _, path := range paths {
			select {
			case <-ctx.Done():
				return
			default:
			}

			info, err := os.Stat(path)
			if err != nil {
				resultsCh <- fileResult{Path: path, Err: err}
				continue
			}

			jobs <- fileJob{
				Path: path,
				Info: info,
				Meta: existing[path],
			}
		}
	}()

	results := make([]fileResult, 0, len(paths))
	for result := range resultsCh {
		atomic.AddInt64(&stats.Processed, 1)
		if result.Err != nil {
			atomic.AddInt64(&stats.ReadErrors, 1)
			continue
		}
		switch result.Reason {
		case "cached":
			atomic.AddInt64(&stats.SkippedCached, 1)
		case "short":
			atomic.AddInt64(&stats.SkippedShort, 1)
		}
		results = append(results, result)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

func processOne(job fileJob, minChars int) fileResult {
	if job.Meta.ID != 0 && job.Meta.Size == job.Info.Size() && job.Meta.ModTime == job.Info.ModTime().UnixNano() {
		return fileResult{
			Path:     job.Path,
			Title:    titleFromPath(job.Path),
			Existing: job.Meta,
			Skip:     true,
			Reason:   "cached",
		}
	}

	raw, err := os.ReadFile(job.Path)
	if err != nil {
		return fileResult{Path: job.Path, Err: err}
	}

	cleaned := cleanText(raw)
	if runeCount(cleaned) < minChars {
		return fileResult{
			Path:     job.Path,
			Title:    titleFromPath(job.Path),
			Existing: job.Meta,
			Skip:     true,
			Reason:   "short",
		}
	}

	hash := sha1Hex(raw)
	return fileResult{
		Path:     job.Path,
		Title:    titleFromPath(job.Path),
		Hash:     hash,
		Content:  cleaned,
		Size:     job.Info.Size(),
		ModTime:  job.Info.ModTime().UnixNano(),
		Existing: job.Meta,
	}
}

func writeResults(db *sql.DB, results []fileResult, existing map[string]fileMeta, opts Options, stats *Stats) error {
	batch, err := newBatch(db)
	if err != nil {
		return err
	}
	defer batch.Close()

	seenHashes := make(map[string]string, len(results))
	for _, meta := range existing {
		if meta.Hash != "" {
			seenHashes[meta.Hash] = meta.Path
		}
	}

	processedPaths := make(map[string]struct{}, len(results))
	mutations := 0
	for _, result := range results {
		processedPaths[result.Path] = struct{}{}

		if result.Skip {
			continue
		}

		if prevPath, ok := seenHashes[result.Hash]; ok && prevPath != result.Path {
			if result.Existing.ID != 0 {
				if _, err := batch.deleteFTSByID.Exec(result.Existing.ID); err != nil {
					return err
				}
				if _, err := batch.deleteDocByID.Exec(result.Existing.ID); err != nil {
					return err
				}
			}
			atomic.AddInt64(&stats.SkippedDup, 1)
			mutations++
			if mutations%opts.CommitEvery == 0 {
				if err := batch.Rotate(db); err != nil {
					return err
				}
			}
			continue
		}

		rowID := result.Existing.ID
		if rowID == 0 {
			execResult, err := batch.insertDoc.Exec(result.Path, result.Hash, result.Title, result.Size, result.ModTime)
			if err != nil {
				return err
			}
			rowID, err = execResult.LastInsertId()
			if err != nil {
				return err
			}
			atomic.AddInt64(&stats.Indexed, 1)
		} else {
			if _, err := batch.updateDoc.Exec(result.Path, result.Hash, result.Title, result.Size, result.ModTime, rowID); err != nil {
				return err
			}
			if _, err := batch.deleteFTSByID.Exec(rowID); err != nil {
				return err
			}
			atomic.AddInt64(&stats.Updated, 1)
		}

		if _, err := batch.insertFTS.Exec(rowID, result.Content, result.Path, result.Title); err != nil {
			return err
		}

		seenHashes[result.Hash] = result.Path
		mutations++
		if mutations%opts.CommitEvery == 0 {
			if err := batch.Rotate(db); err != nil {
				return err
			}
		}
	}

	if opts.PruneDeleted {
		for path, meta := range existing {
			if _, ok := processedPaths[path]; ok {
				continue
			}
			if _, err := batch.deleteFTSByID.Exec(meta.ID); err != nil {
				return err
			}
			if _, err := batch.deleteDocByID.Exec(meta.ID); err != nil {
				return err
			}
			atomic.AddInt64(&stats.DeletedMissing, 1)
		}
	}

	return batch.Commit()
}

type writeBatch struct {
	tx            *sql.Tx
	insertDoc     *sql.Stmt
	updateDoc     *sql.Stmt
	deleteDocByID *sql.Stmt
	deleteFTSByID *sql.Stmt
	insertFTS     *sql.Stmt
}

func newBatch(db *sql.DB) (*writeBatch, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}

	insertDoc, err := tx.Prepare(`INSERT INTO docs(path, hash, title, size_bytes, mod_time_ns) VALUES(?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	updateDoc, err := tx.Prepare(`UPDATE docs SET path = ?, hash = ?, title = ?, size_bytes = ?, mod_time_ns = ? WHERE id = ?`)
	if err != nil {
		_ = insertDoc.Close()
		_ = tx.Rollback()
		return nil, err
	}
	deleteDocByID, err := tx.Prepare(`DELETE FROM docs WHERE id = ?`)
	if err != nil {
		_ = insertDoc.Close()
		_ = updateDoc.Close()
		_ = tx.Rollback()
		return nil, err
	}
	deleteFTSByID, err := tx.Prepare(`DELETE FROM docs_fts WHERE rowid = ?`)
	if err != nil {
		_ = insertDoc.Close()
		_ = updateDoc.Close()
		_ = deleteDocByID.Close()
		_ = tx.Rollback()
		return nil, err
	}
	insertFTS, err := tx.Prepare(`INSERT INTO docs_fts(rowid, content, path, title) VALUES(?, ?, ?, ?)`)
	if err != nil {
		_ = insertDoc.Close()
		_ = updateDoc.Close()
		_ = deleteDocByID.Close()
		_ = deleteFTSByID.Close()
		_ = tx.Rollback()
		return nil, err
	}

	return &writeBatch{
		tx:            tx,
		insertDoc:     insertDoc,
		updateDoc:     updateDoc,
		deleteDocByID: deleteDocByID,
		deleteFTSByID: deleteFTSByID,
		insertFTS:     insertFTS,
	}, nil
}

func (b *writeBatch) Rotate(db *sql.DB) error {
	if err := b.Commit(); err != nil {
		return err
	}
	next, err := newBatch(db)
	if err != nil {
		return err
	}
	*b = *next
	return nil
}

func (b *writeBatch) Commit() error {
	if b == nil || b.tx == nil {
		return nil
	}
	err := b.tx.Commit()
	b.closeStatements()
	b.tx = nil
	return err
}

func (b *writeBatch) Close() {
	if b == nil {
		return
	}
	b.closeStatements()
	if b.tx != nil {
		_ = b.tx.Rollback()
		b.tx = nil
	}
}

func (b *writeBatch) closeStatements() {
	if b.insertDoc != nil {
		_ = b.insertDoc.Close()
		b.insertDoc = nil
	}
	if b.updateDoc != nil {
		_ = b.updateDoc.Close()
		b.updateDoc = nil
	}
	if b.deleteDocByID != nil {
		_ = b.deleteDocByID.Close()
		b.deleteDocByID = nil
	}
	if b.deleteFTSByID != nil {
		_ = b.deleteFTSByID.Close()
		b.deleteFTSByID = nil
	}
	if b.insertFTS != nil {
		_ = b.insertFTS.Close()
		b.insertFTS = nil
	}
}

func extensionSet(input []string) map[string]struct{} {
	set := make(map[string]struct{}, len(input))
	for _, ext := range input {
		ext = strings.TrimSpace(strings.ToLower(ext))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		set[ext] = struct{}{}
	}
	return set
}

func cleanText(input []byte) string {
	text := stripUTF8BOM(string(input))
	text = stripFrontMatter(text)
	text = stripTripleBacktickBlocks(text)
	text = collapseBlankLines(text)
	return strings.TrimSpace(text)
}

func stripUTF8BOM(s string) string {
	return strings.TrimPrefix(s, "\ufeff")
}

func stripFrontMatter(s string) string {
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return s
	}

	rest := s[4:]
	if strings.HasPrefix(s, "---\r\n") {
		rest = s[5:]
	}
	if idx := strings.Index(rest, "\n---\n"); idx >= 0 {
		return rest[idx+5:]
	}
	if idx := strings.Index(rest, "\r\n---\r\n"); idx >= 0 {
		return rest[idx+7:]
	}
	return s
}

func stripTripleBacktickBlocks(s string) string {
	var out strings.Builder
	inBlock := false
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inBlock = !inBlock
			if !inBlock {
				out.WriteString("\n")
			}
			continue
		}
		if inBlock {
			continue
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String()
}

func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	var out strings.Builder
	blank := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if blank {
				continue
			}
			blank = true
			out.WriteString("\n")
			continue
		}
		blank = false
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String()
}

func runeCount(s string) int {
	return len([]rune(s))
}

func sha1Hex(data []byte) string {
	h := sha1.New()
	_, _ = io.Copy(h, bytes.NewReader(data))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func titleFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
