package logger

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
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
	fmt.Printf("Captured source info:\n")
	fmt.Printf("  File: %s\n", handler.lastSource.File)
	fmt.Printf("  Function: %s\n", handler.lastSource.Function)
	fmt.Printf("  Line: %d\n", handler.lastSource.Line)
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

	fmt.Printf("Error case source info:\n")
	fmt.Printf("  File: %s\n", handler.lastSource.File)
	fmt.Printf("  Function: %s\n", handler.lastSource.Function)
	fmt.Printf("  Line: %d\n", handler.lastSource.Line)
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

	fmt.Printf("Nested call source info:\n")
	fmt.Printf("  File: %s\n", handler.lastSource.File)
	fmt.Printf("  Function: %s\n", handler.lastSource.Function)
	fmt.Printf("  Line: %d\n", handler.lastSource.Line)
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

	fmt.Printf("Context values test:\n")
	fmt.Printf("  SQL: %s\n", expectedSQL)
	fmt.Printf("  Rows: %d\n", expectedRows)
	fmt.Printf("  Duration: >= 100ms\n")
}

// Тест для проверки вывода dbresolver режима [source]
func TestDBResolverSourceMode(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := NewDevHandler(Options{W: buf, Source: true})
	testLogger := slog.New(handler)
	slog.SetDefault(testLogger)

	gormLog := NewGormLogger(true, nil)

	ctx := context.WithValue(context.Background(), DBResolverMode, "source")
	begin := time.Now()
	fc := func() (string, int64) {
		return "INSERT INTO users (name) VALUES ('John')", 1
	}

	gormLog.Trace(ctx, begin, fc, nil)

	output := buf.String()
	fmt.Printf("=== DBResolver Source Mode Test ===\n")
	fmt.Printf("%s", output)

	if !strings.Contains(output, "[source]") {
		t.Errorf("Expected output to contain '[source]', got: %s", output)
	}
}

// Тест для проверки вывода dbresolver режима [replica]
func TestDBResolverReplicaMode(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := NewDevHandler(Options{W: buf, Source: true})
	testLogger := slog.New(handler)
	slog.SetDefault(testLogger)

	gormLog := NewGormLogger(true, nil)

	ctx := context.WithValue(context.Background(), DBResolverMode, "replica")
	begin := time.Now()
	fc := func() (string, int64) {
		return "SELECT * FROM users WHERE id = ?", 10
	}

	gormLog.Trace(ctx, begin, fc, nil)

	output := buf.String()
	fmt.Printf("=== DBResolver Replica Mode Test ===\n")
	fmt.Printf("%s", output)

	if !strings.Contains(output, "[replica]") {
		t.Errorf("Expected output to contain '[replica]', got: %s", output)
	}
}

// Тест для проверки цвета DEBUG уровня
func TestDebugLevelColor(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := NewDevHandler(Options{W: buf, Source: true})
	testLogger := slog.New(handler)
	slog.SetDefault(testLogger)

	// Логируем на уровне DEBUG
	slog.DebugContext(context.Background(), "Debug test message", slog.String("key", "value"))

	output := buf.String()
	fmt.Printf("=== DEBUG Level Color Test ===\n")
	fmt.Printf("%s", output)
	fmt.Printf("(DEBUG должен быть синим/голубым цветом)\n")

	if !strings.Contains(output, "DEBUG") {
		t.Errorf("Expected output to contain 'DEBUG', got: %s", output)
	}
}

// Тест для проверки правильного определения пути вызова через обертки
func TestLoggerWrapperSource(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := NewDevHandler(Options{W: buf, Source: true})
	testLogger := slog.New(handler)
	slog.SetDefault(testLogger)

	// Вызываем через обертку
	Info("Test info message")

	output := buf.String()
	fmt.Printf("=== Logger Wrapper Source Test ===\n")
	fmt.Printf("%s", output)
	fmt.Printf("(Путь должен указывать на эту функцию, а не на библиотеку logger)\n")

	// Проверяем что путь указывает на этот тест, а не на logger
	if strings.Contains(output, "logger.go") {
		t.Errorf("Source should not point to logger.go, got: %s", output)
	}
	if !strings.Contains(output, "gorm_test.go") {
		t.Errorf("Source should point to gorm_test.go, got: %s", output)
	}

	// Очищаем буфер для следующего теста
	buf.Reset()

	// Тестируем Debug
	Debug("Test debug message")
	output = buf.String()
	fmt.Printf("\nDebug output:\n%s", output)

	if !strings.Contains(output, "gorm_test.go") {
		t.Errorf("Debug source should point to gorm_test.go, got: %s", output)
	}

	// Очищаем буфер
	buf.Reset()

	// Тестируем Warn
	Warn("Test warn message")
	output = buf.String()
	fmt.Printf("\nWarn output:\n%s", output)

	if !strings.Contains(output, "gorm_test.go") {
		t.Errorf("Warn source should point to gorm_test.go, got: %s", output)
	}

	// Очищаем буфер
	buf.Reset()

	// Тестируем Error
	Error("Test error message")
	output = buf.String()
	fmt.Printf("\nError output:\n%s", output)

	if !strings.Contains(output, "gorm_test.go") {
		t.Errorf("Error source should point to gorm_test.go, got: %s", output)
	}
}

// === Тесты для *Once функций ===

func resetOnceLog() {
	lastLogKey = ""
}

func TestOnceSameLine(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := NewDevHandler(Options{W: buf, Source: true})
	slog.SetDefault(slog.New(handler))
	resetOnceLog()

	fmt.Println("\n=== TestOnceSameLine ===")
	fmt.Println("3 вызова ErrorOnce с одной строки:")

	for i := 0; i < 3; i++ {
		ErrorOnce("same line error", "i", i) // все 3 вызова с одной строки
	}

	output := buf.String()
	fmt.Printf("%s", output)

	lines := strings.Count(output, "ERROR")
	if lines != 1 {
		t.Errorf("Expected exactly 1 ERROR log, got %d", lines)
	}
	fmt.Println("→ Выведена только 1 запись из 3 ✓")
}

func TestOnceDifferentLines(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := NewDevHandler(Options{W: buf, Source: true})
	slog.SetDefault(slog.New(handler))
	resetOnceLog()

	fmt.Println("\n=== TestOnceDifferentLines ===")
	fmt.Println("3 вызова с разных строк:")

	WarnOnce("first warning")
	WarnOnce("second warning")
	WarnOnce("third warning")

	output := buf.String()
	fmt.Printf("%s", output)

	lines := strings.Count(output, "WARN")
	if lines != 3 {
		t.Errorf("Expected 3 WARN logs, got %d", lines)
	}
	fmt.Println("→ Все 3 записи выведены ✓")
}

// helperA и helperB — вспомогательные функции для тестирования чередования.
// Каждая вызывает *Once из фиксированной строки, чтобы дедупликация работала.
func helperA() { InfoOnce("from A") }
func helperB() { InfoOnce("from B") }

func TestOnceInterleaved(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := NewDevHandler(Options{W: buf, Source: true})
	slog.SetDefault(slog.New(handler))
	resetOnceLog()

	fmt.Println("\n=== TestOnceInterleaved ===")
	fmt.Println("Чередование: A, A, B, A:")

	helperA() // A — выводит
	helperA() // A — пропуск (та же строка)
	helperB() // B — выводит (другая строка)
	helperA() // A — выводит (после B снова уникальна)

	output := buf.String()
	fmt.Printf("%s", output)

	lines := strings.Count(output, "INFO")
	if lines != 3 {
		t.Errorf("Expected 3 INFO logs, got %d", lines)
	}
	fmt.Println("→ 3 записи: A, B, A ✓")
}

func callErrorCtxOnce(ctx context.Context, msg string) {
	ErrorContextOnce(ctx, msg, "key", "val")
}

func TestOnceContext(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := NewDevHandler(Options{W: buf, Source: true, AddCxtAttr: []string{"user_id"}})
	slog.SetDefault(slog.New(handler))
	resetOnceLog()

	ctx := context.WithValue(context.Background(), "user_id", "123")

	fmt.Println("\n=== TestOnceContext ===")
	fmt.Println("ContextOnce с контекстом user_id:")

	callErrorCtxOnce(ctx, "ctx error 1") // выводит
	callErrorCtxOnce(ctx, "ctx error 2") // пропуск
	callErrorCtxOnce(ctx, "ctx error 3") // пропуск

	output := buf.String()
	fmt.Printf("%s", output)

	lines := strings.Count(output, "ERROR")
	if lines != 1 {
		t.Errorf("Expected 1 ERROR log, got %d", lines)
	}
	if !strings.Contains(output, "user_id") || !strings.Contains(output, "123") {
		t.Error("Expected output to contain user_id and 123")
	}
	fmt.Println("→ 1 запись с контекстом ✓")
}

func TestOnceConcurrent(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := NewDevHandler(Options{W: buf, Source: true})
	slog.SetDefault(slog.New(handler))
	resetOnceLog()

	fmt.Println("\n=== TestOnceConcurrent ===")
	fmt.Println("10 горутин, каждая пишет ErrorOnce 100 раз:")

	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				ErrorOnce("concurrent error", "goroutine", id, "i", i)
			}
		}(g)
	}
	wg.Wait()

	output := buf.String()
	fmt.Printf("%s", output)

	lines := strings.Count(output, "ERROR")
	fmt.Printf("→ Всего записей: %d (из 1000 возможных)\n", lines)

	if lines == 0 {
		t.Error("Expected at least 1 log")
	}
	if lines > 10 {
		t.Errorf("Expected at most 10 logs (one per goroutine), got %d", lines)
	}
}

func TestOnceContextConcurrent(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := NewDevHandler(Options{W: buf, Source: true})
	slog.SetDefault(slog.New(handler))
	resetOnceLog()

	fmt.Println("\n=== TestOnceContextConcurrent ===")
	fmt.Println("5 горутин с разными контекстами:")

	var wg sync.WaitGroup
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ctx := context.WithValue(context.Background(), "req_id", fmt.Sprintf("req-%d", id))
			for i := 0; i < 50; i++ {
				WarnContextOnce(ctx, "concurrent warn", "goroutine", id)
			}
		}(g)
	}
	wg.Wait()

	output := buf.String()
	fmt.Printf("%s", output)

	lines := strings.Count(output, "WARN")
	fmt.Printf("→ Всего записей: %d\n", lines)

	if lines == 0 {
		t.Error("Expected at least 1 log")
	}
}
