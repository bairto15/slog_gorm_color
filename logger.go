package logger

import (
	"context"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	Source   = "source"
	Duration = "duration"
	Rows     = "rows"
	Sql      = "sql"
)

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
		if c := ctx.Value(Source); c == nil {
			fs := runtime.CallersFrames([]uintptr{rec.PC})
			f, _ := fs.Next()
			if f.File != "" {
				dir, file := filepath.Split(f.File)
				pathFile := path.Join(filepath.Base(dir), file)

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
