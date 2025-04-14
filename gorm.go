package logger

import (
	"context"
	"log/slog"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm/logger"
	"gorm.io/gorm/utils"
)

type gormLogger struct {
	logger.Config

	logger *slog.Logger
}

func NewGormLogger(showParams bool, args ...any) logger.Interface {
	log := slog.With(args)

	if showParams {
		return &gormLogger{
			Config: logger.Config{LogLevel: logger.Info},
			logger: log,
		}
	}

	return &withOutParams{
		gormLogger: gormLogger{
			Config: logger.Config{LogLevel: logger.Info},
			logger: log,
		},
	}
}

// Имплементация интерфейса gorm логера
func (g *gormLogger) LogMode(logLevel logger.LogLevel) logger.Interface {
	newLogger := *g
	newLogger.LogLevel = logLevel
	return &newLogger
}

func (g *gormLogger) Info(ctx context.Context, msg string, data ...any) {
	g.logger.InfoContext(ctx, msg, data...)
}

func (g *gormLogger) Warn(ctx context.Context, msg string, data ...any) {
	g.logger.WarnContext(ctx, msg, data...)
}

func (g *gormLogger) Error(ctx context.Context, msg string, data ...any) {
	g.logger.ErrorContext(ctx, msg, data...)
}

func (g *gormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, rows := fc()

	ctx = context.WithValue(ctx, Sql, sql)
	ctx = context.WithValue(ctx, Rows, rows)

	duration := time.Since(begin)
	ctx = context.WithValue(ctx, Duration, duration)

	pathLine := utils.FileWithLineNum()
	dir, file := filepath.Split(pathLine)

	file, line := getFileLine(file)

	funcName := getGormFuncName()

	pathFile := path.Join(filepath.Base(dir), file)

	source := slog.Source{
		Function: funcName,
		File:     pathFile,
		Line:     line,
	}

	ctx = context.WithValue(ctx, Source, source)

	if err != nil {
		g.Error(ctx, err.Error())
		return
	}

	g.Info(ctx, "")
}

type withOutParams struct {
	gormLogger
}

func (g *withOutParams) ParamsFilter(ctx context.Context, sql string, params ...any) (string, []any) {
	return sql, nil
}

func getGormFuncName() string {
	pcs := [13]uintptr{}

	length := runtime.Callers(3, pcs[:])
	frames := runtime.CallersFrames(pcs[:length])

	for i := 0; i < length; i++ {
		frame, _ := frames.Next()

		if (!strings.Contains(frame.Function, "gorm.io/gorm") || strings.HasSuffix(frame.File, "_test.go")) && !strings.HasSuffix(frame.File, ".gen.go") {
			return strings.Replace(path.Ext(frame.Function), ".", "", 1)
		}
	}

	return ""
}

func getFileLine(fileLine string) (file string, line int) {
	arr := strings.Split(fileLine, ":")
	if len(arr) != 2 {
		return
	}

	file = arr[0]
	line, err := strconv.Atoi(arr[1])
	if err != nil {
		slog.Error(err.Error())
		return
	}

	return
}
