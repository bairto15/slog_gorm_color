package logger

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	Source         = "source"
	Duration       = "duration"
	Rows           = "rows"
	Sql            = "sql"
	Resolver       = "resolver"
	DBResolverMode = "dbresolver:resolver_mode_key"
)

var rootDir string

func initRootDir(customDir string) {
	if customDir != "" {
		rootDir = customDir
		return
	}
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			rootDir = dir
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
}

func relativePath(abs string) string {
	if rootDir == "" {
		return abs
	}
	rel, err := filepath.Rel(rootDir, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return abs
	}
	return rel
}

type HandlerMiddleware struct {
	source     bool
	addCxtAttr []string
	next       slog.Handler
}

func NewHandlerMiddleware(next slog.Handler, opt Options) *HandlerMiddleware {
	return &HandlerMiddleware{next: next, source: opt.Source, addCxtAttr: opt.AddCxtAttr}
}

func (h *HandlerMiddleware) Enabled(ctx context.Context, rec slog.Level) bool {
	return h.next.Enabled(ctx, rec)
}

func (h *HandlerMiddleware) Handle(ctx context.Context, rec slog.Record) error {
	for _, v := range h.addCxtAttr {
		if c := ctx.Value(v); c != nil {
			rec.Add(v, c)
		}
	}

	if c := ctx.Value(Sql); c != nil {
		rec.Add(Sql, c)
	}

	if h.source {
		if c := ctx.Value(Source); c != nil {
			rec.Add(string(Source), c)
		} else {
			fs := runtime.CallersFrames([]uintptr{rec.PC})
			f, _ := fs.Next()
			if f.File != "" {
				pathFile := relativePath(f.File)

				src := &slog.Source{
					Function: getFuncNameSlog(f.Function),
					File:     pathFile,
					Line:     f.Line,
				}

				rec.Add(string(Source), src)
			}
		}
	}

	return h.next.Handle(ctx, rec)
}

func (h *HandlerMiddleware) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &HandlerMiddleware{next: h.next.WithAttrs(attrs)}
}

func (h *HandlerMiddleware) WithGroup(name string) slog.Handler {
	return &HandlerMiddleware{next: h.next.WithGroup(name)}
}

func InitLogger(opts Options) {
	initRootDir(opts.RootDir)
	opt := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}

	handler := slog.Handler(slog.NewJSONHandler(os.Stdout, opt))
	handler = NewHandlerMiddleware(handler, opts)

	logger := slog.New(handler)

	slog.SetDefault(logger)
}

func GetLogger() *slog.Logger {
	return slog.Default()
}

func InitDevLogger(opts Options) {
	initRootDir(opts.RootDir)
	handler := NewDevHandler(opts)

	logger := slog.New(handler)

	slog.SetDefault(logger)
}

func getFuncNameSlog(pathFunc string) string {
	arr := strings.Split(pathFunc, ".")

	if len(arr) == 0 {
		return pathFunc
	}

	var funcName string

	for i := len(arr) - 1; i >= 0; i-- {
		_, err := strconv.Atoi(arr[i])

		if strings.HasPrefix(arr[i], "func") || err == nil {
			funcName = "." + arr[i] + funcName
			continue
		}

		funcName = arr[i] + funcName
		break
	}

	return funcName
}

// getCallerInfo получает информацию о вызывающем коде, пропуская skip фреймов
func getCallerInfo(skip int) slog.Source {
	pcs := [1]uintptr{}
	n := runtime.Callers(skip, pcs[:])
	if n < 1 {
		return slog.Source{}
	}

	frames := runtime.CallersFrames(pcs[:n])
	frame, _ := frames.Next()

	if frame.File == "" {
		return slog.Source{}
	}

	return slog.Source{
		Function: getFuncNameSlog(frame.Function),
		File:     relativePath(frame.File),
		Line:     frame.Line,
	}
}

// Info логирует сообщение на уровне Info с правильным определением места вызова
func Info(msg string, args ...any) {
	src := getCallerInfo(3)
	ctx := context.WithValue(context.Background(), Source, src)
	slog.InfoContext(ctx, msg, args...)
}

// Debug логирует сообщение на уровне Debug с правильным определением места вызова
func Debug(msg string, args ...any) {
	src := getCallerInfo(3)
	ctx := context.WithValue(context.Background(), Source, src)
	slog.DebugContext(ctx, msg, args...)
}

// Warn логирует сообщение на уровне Warn с правильным определением места вызова
func Warn(msg string, args ...any) {
	src := getCallerInfo(3)
	ctx := context.WithValue(context.Background(), Source, src)
	slog.WarnContext(ctx, msg, args...)
}

// Error логирует сообщение на уровне Error с правильным определением места вызова
func Error(msg string, args ...any) {
	src := getCallerInfo(3)
	ctx := context.WithValue(context.Background(), Source, src)
	slog.ErrorContext(ctx, msg, args...)
}
