package logger

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm/logger"
)

// Тестовая структура для перехвата логов
type testLogHandler struct {
	lastSource *slog.Source
	lastCtx    context.Context
}

func (t *testLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (t *testLogHandler) Handle(ctx context.Context, record slog.Record) error {
	t.lastCtx = ctx
	// Извлекаем source из контекста
	if src := ctx.Value(Source); src != nil {
		if source, ok := src.(slog.Source); ok {
			t.lastSource = &source
		}
	}
	return nil
}

func (t *testLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return t
}

func (t *testLogHandler) WithGroup(name string) slog.Handler {
	return t
}

// Вспомогательная функция для имитации вызова из пользовательского кода
func testDatabaseQuery(gl logger.Interface) {
	ctx := context.Background()
	begin := time.Now()

	fc := func() (string, int64) {
		return "SELECT * FROM users WHERE id = ?", 1
	}

	// Вызываем Trace - именно здесь должны быть корректные данные
	gl.Trace(ctx, begin, fc, nil)
}

func TestGormLoggerSourceInfo(t *testing.T) {
	// Создаем тестовый обработчик
	handler := &testLogHandler{}

	// Устанавливаем его как логгер по умолчанию
	testLogger := slog.New(handler)
	slog.SetDefault(testLogger)

	// Создаем gorm логгер
	gormLog := NewGormLogger(true, nil)

	// Вызываем функцию, которая использует gorm логгер
	testDatabaseQuery(gormLog)

	// Проверяем, что source был установлен
	if handler.lastSource == nil {
		t.Fatal("Source information was not captured")
	}

	// Проверяем имя файла
	if !strings.Contains(handler.lastSource.File, "gorm_test.go") {
		t.Errorf("Expected file to contain 'gorm_test.go', got: %s", handler.lastSource.File)
	}

	// Проверяем имя функции
	if handler.lastSource.Function != "testDatabaseQuery" {
		t.Errorf("Expected function name 'testDatabaseQuery', got: %s", handler.lastSource.Function)
	}

	// Проверяем номер строки (должен быть в районе вызова Trace)
	if handler.lastSource.Line == 0 {
		t.Error("Line number should not be 0")
	}

	// Выводим информацию для визуального контроля
	t.Logf("Captured source info:")
	t.Logf("  File: %s", handler.lastSource.File)
	t.Logf("  Function: %s", handler.lastSource.Function)
	t.Logf("  Line: %d", handler.lastSource.Line)
}

func TestGormLoggerWithError(t *testing.T) {
	handler := &testLogHandler{}
	testLogger := slog.New(handler)
	slog.SetDefault(testLogger)

	gormLog := NewGormLogger(true, nil)

	ctx := context.Background()
	begin := time.Now()
	fc := func() (string, int64) {
		return "SELECT * FROM invalid_table", 0
	}

	// Вызываем с ошибкой
	gormLog.Trace(ctx, begin, fc, context.DeadlineExceeded)

	if handler.lastSource == nil {
		t.Fatal("Source information was not captured for error case")
	}

	t.Logf("Error case source info:")
	t.Logf("  File: %s", handler.lastSource.File)
	t.Logf("  Function: %s", handler.lastSource.Function)
	t.Logf("  Line: %d", handler.lastSource.Line)
}

// Тест для проверки вложенных вызовов
func helperFunction(gl logger.Interface) {
	testDatabaseQuery(gl)
}

func TestNestedCalls(t *testing.T) {
	handler := &testLogHandler{}
	testLogger := slog.New(handler)
	slog.SetDefault(testLogger)

	gormLog := NewGormLogger(true, nil)

	// Вызываем через вспомогательную функцию
	helperFunction(gormLog)

	if handler.lastSource == nil {
		t.Fatal("Source information was not captured in nested call")
	}

	// Функция должна быть testDatabaseQuery, а не helperFunction
	if handler.lastSource.Function != "testDatabaseQuery" {
		t.Errorf("Expected function name 'testDatabaseQuery' in nested call, got: %s", handler.lastSource.Function)
	}

	t.Logf("Nested call source info:")
	t.Logf("  File: %s", handler.lastSource.File)
	t.Logf("  Function: %s", handler.lastSource.Function)
	t.Logf("  Line: %d", handler.lastSource.Line)
}

// Тест для проверки корректности context values
func TestContextValues(t *testing.T) {
	handler := &testLogHandler{}
	testLogger := slog.New(handler)
	slog.SetDefault(testLogger)

	gormLog := NewGormLogger(true, nil)

	ctx := context.Background()
	begin := time.Now().Add(-100 * time.Millisecond) // Симулируем задержку

	expectedSQL := "SELECT * FROM users WHERE id = ?"
	expectedRows := int64(5)

	fc := func() (string, int64) {
		return expectedSQL, expectedRows
	}

	gormLog.Trace(ctx, begin, fc, nil)

	// Проверяем SQL
	if sql := handler.lastCtx.Value(Sql); sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got: %v", expectedSQL, sql)
	}

	// Проверяем Rows
	if rows := handler.lastCtx.Value(Rows); rows != expectedRows {
		t.Errorf("Expected rows %d, got: %v", expectedRows, rows)
	}

	// Проверяем Duration
	if duration := handler.lastCtx.Value(Duration); duration == nil {
		t.Error("Duration should be set")
	} else {
		d, ok := duration.(time.Duration)
		if !ok {
			t.Error("Duration should be of type time.Duration")
		} else if d < 100*time.Millisecond {
			t.Errorf("Duration should be at least 100ms, got: %v", d)
		}
	}
}
