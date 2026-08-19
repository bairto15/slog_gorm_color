package logger

import (
	"context"
	"log/slog"
	"runtime"
	"strings"
	"time"

	"gorm.io/gorm/logger"
)

type gormLogger struct {
	logger.Config
	attr []slog.Attr
}

func NewGormLogger(showParams bool, attr []slog.Attr) logger.Interface {
	l := &gormLogger{
		Config: logger.Config{LogLevel: logger.Info},
		attr:   attr,
	}

	if showParams {
		return l
	}

	return &withOutParams{gormLogger: l}
}

// Имплементация интерфейса gorm логера
func (g *gormLogger) LogMode(logLevel logger.LogLevel) logger.Interface {
	newLogger := *g
	newLogger.LogLevel = logLevel
	return &newLogger
}

func (g *gormLogger) Info(ctx context.Context, msg string, data ...any) {
	slog.InfoContext(ctx, msg, data...)
}

func (g *gormLogger) Warn(ctx context.Context, msg string, data ...any) {
	slog.WarnContext(ctx, msg, data...)
}

func (g *gormLogger) Error(ctx context.Context, msg string, data ...any) {
	slog.ErrorContext(ctx, msg, data...)
}

func (g *gormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, rows := fc()

	ctx = context.WithValue(ctx, Sql, sql)
	ctx = context.WithValue(ctx, Rows, rows)

	duration := time.Since(begin)
	ctx = context.WithValue(ctx, Duration, duration)

	// Извлекаем режим dbresolver (source/replica) из контекста
	if mode := ctx.Value(DBResolverMode); mode != nil {
		ctx = context.WithValue(ctx, Resolver, mode)
	}

	funcName, file, line := getGormFuncName()

	source := slog.Source{
		Function: funcName,
		File:     file,
		Line:     line,
	}

	ctx = context.WithValue(ctx, Source, source)

	if err != nil {
		g.Error(ctx, err.Error())
		return
	}

	slog.LogAttrs(ctx, slog.LevelInfo, "", g.attr...)
}

type withOutParams struct {
	*gormLogger
}

func (g *withOutParams) ParamsFilter(ctx context.Context, sql string, params ...any) (string, []any) {
	return sql, nil
}

func getGormFuncName() (funcName string, file string, line int) {
	pcs := [20]uintptr{}

	// skip=3 пропускает: runtime.Callers -> getGormFuncName -> Trace
	length := runtime.Callers(3, pcs[:])
	frames := runtime.CallersFrames(pcs[:length])

	for i := 0; i < length; i++ {
		frame, _ := frames.Next()

		isGorm := strings.Contains(frame.Function, "gorm.io/gorm")
		isGen := strings.HasSuffix(frame.File, ".gen.go")
		isRuntime := strings.HasPrefix(frame.Function, "runtime.")
		isTesting := strings.HasPrefix(frame.Function, "testing.")

		if isGorm || isGen || isRuntime || isTesting {
			continue
		}

		// Извлекаем имя функции корректно
		funcName = extractFuncName(frame.Function)

		file = relativePath(frame.File)
		line = frame.Line

		return
	}

	return "", "", 0
}

// extractFuncName корректно извлекает имя функции из полного пути
func extractFuncName(fullName string) string {
	// Убираем пакет, оставляем только тип и метод
	parts := strings.Split(fullName, "/")
	if len(parts) == 0 {
		return fullName
	}

	// Берём последнюю часть (package.Func или package.(*Type).Method)
	lastPart := parts[len(parts)-1]

	// Убираем имя пакета
	dotIdx := strings.Index(lastPart, ".")
	if dotIdx == -1 || dotIdx == len(lastPart)-1 {
		return lastPart
	}

	return lastPart[dotIdx+1:]
}
