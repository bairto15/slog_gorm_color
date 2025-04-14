package logger

import (
	"context"
	"encoding"
	"fmt"
	"io"
	"log/slog"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)


const (
	// ANSI-коды для цветов
	Reset        = "\u001b[0m"
	Red          = "\u001b[31m"
	Faint        = "\u001b[90m"
	Green        = "\u001b[32m"
	Yellow       = "\u001b[33m"
	YellowBack   = "\u001b[43m"
	Blue         = "\u001b[34m"
	Magenta      = "\u001b[35m"
	Cyan         = "\u001b[36m"
	BrightGreen  = "\u001b[92m"
	BrightYellow = "\u001b[93m"

	ansiEsc = '\u001b'	
)

type Options struct {
	AddCxtAttr    []string
	W             io.Writer
	Source        bool
	SlowThreshold time.Duration
}

type handlerTextColor struct {
	source      bool
	timeFormat  string
	level       slog.Leveler
	attrsPrefix string
	groupPrefix string
	addCxtAttr  []string
	groups      []string

	slowThreshold time.Duration

	mu sync.Mutex
	w  io.Writer
}

func NewDevHandler(opt Options) slog.Handler {
	if opt.SlowThreshold == 0 {
		opt.SlowThreshold = time.Second
	}

	return &handlerTextColor{
		level:         slog.LevelDebug,
		timeFormat:    time.TimeOnly,
		source:        opt.Source,
		slowThreshold: opt.SlowThreshold,
		addCxtAttr:    opt.AddCxtAttr,
		w:             opt.W,
	}
}

func (h *handlerTextColor) clone() *handlerTextColor {
	return &handlerTextColor{
		attrsPrefix: h.attrsPrefix,
		groupPrefix: h.groupPrefix,
		groups:      h.groups,
		w:           h.w,
		level:       h.level,
		timeFormat:  h.timeFormat,
	}
}

func (h *handlerTextColor) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (h *handlerTextColor) Handle(ctx context.Context, r slog.Record) error {
	buf := newBuffer()
	defer buf.Free()

	// write time log
	if !r.Time.IsZero() {
		h.appendTime(buf, r.Time)
		buf.WriteByte(' ')
	}

	// write level
	h.appendLevel(buf, r.Level)
	buf.WriteByte(' ')

	// write path and line call
	fs := runtime.CallersFrames([]uintptr{r.PC})
	f, _ := fs.Next()
	if h.source && f.File != "" {		
		if c, ok := ctx.Value(Source).(slog.Source); ok {
			h.appendSource(buf, &c)
		} else {
			src := &slog.Source{
				Function: f.Function,
				File:     f.File,
				Line:     f.Line,
			}

			h.appendSource(buf, src)
		}
	}

	// write message
	h.appendMessage(buf, r.Level, r.Message)

	// write attributes
	r.Attrs(func(attr slog.Attr) bool {
		h.appendAttr(buf, attr, h.groupPrefix, h.groups)
		return true
	})

	// write context values
	h.AddValueCtx(ctx, buf)

	// write handlerTextColor attributes
	if len(h.attrsPrefix) > 0 {
		buf.WriteString(h.attrsPrefix)
	}

	// write sql
	h.appendSql(ctx, r.Level, buf)

	if len(*buf) == 0 {
		return nil
	}
	(*buf)[len(*buf)-1] = '\n' // replace last space with newline

	h.mu.Lock()
	defer h.mu.Unlock()

	_, err := h.w.Write(*buf)
	return err
}

func (h *handlerTextColor) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}

	h2 := h.clone()

	buf := newBuffer()
	defer buf.Free()

	// write attributes to buffer
	for _, attr := range attrs {
		h.appendAttr(buf, attr, h.groupPrefix, h.groups)
	}
	h2.attrsPrefix = h.attrsPrefix + string(*buf)
	return h2
}

func (h *handlerTextColor) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	h2 := h.clone()
	h2.groupPrefix += name + "."
	h2.groups = append(h2.groups, name)
	return h2
}

func (h *handlerTextColor) AddValueCtx(ctx context.Context, buf *buffer) error {
	for _, v := range h.addCxtAttr {
		if c := ctx.Value(v); c != nil {
			h.appendCtxValue(buf, v, fmt.Sprintf("%v ", c))
		}
	}

	return nil
} 

func (h *handlerTextColor) appendTime(buf *buffer, t time.Time) {
	buf.WriteString(Faint)
	*buf = t.AppendFormat(*buf, h.timeFormat)
	buf.WriteString(Reset)
}

func (h *handlerTextColor) appendLevel(buf *buffer, level slog.Level) {
	colorLevel := Red
	if level.Level() == slog.LevelInfo {
		colorLevel = BrightGreen
	} else if level.Level() == slog.LevelWarn {
		colorLevel = BrightYellow
	}

	buf.WriteString(colorLevel)
	buf.WriteString(level.String())
	buf.WriteString(Reset)
}

func (h *handlerTextColor) appendSource(buf *buffer, src *slog.Source) {
	dir, file := filepath.Split(src.File)

	buf.WriteString(Faint)
	buf.WriteString(path.Join(filepath.Base(dir), file))

	if src.Line != 0 {
		buf.WriteByte(':')
		buf.WriteString(strconv.Itoa(src.Line))
		buf.WriteString(Reset)
	}

	buf.WriteString(" ")

	buf.WriteString(Blue)
	buf.WriteString(getFuncNameSlog(src.Function))
	buf.WriteString(Reset)

	buf.WriteByte(' ')
}

func (h *handlerTextColor) appendMessage(buf *buffer, level slog.Level, msg string) {
	if msg == "" {
		return
	}

	colorMsg := Cyan
	if level == slog.LevelError {
		colorMsg = Red
	}

	buf.WriteString(colorMsg)
	buf.WriteString(msg)
	buf.WriteString(Reset)
	buf.WriteString(" ")
}

func (h *handlerTextColor) appendSql(ctx context.Context, level slog.Level, buf *buffer) {
	sql := ctx.Value(Sql)
	if sql == nil {
		return
	}

	buf.WriteString("\n")

	if c, ok := ctx.Value(Duration).(time.Duration); ok {
		colorDuration := Green

		if c > h.slowThreshold {
			colorDuration = Red
		}

		duration := c.Seconds()
		durStr := strconv.FormatFloat(duration, 'f', 4, 64)

		buf.WriteString(colorDuration)
		buf.WriteString(fmt.Sprintf("[%v] ", durStr))
		buf.WriteString(Reset)
	}

	if c := ctx.Value(Rows); c != nil {
		buf.WriteString(Yellow)
		buf.WriteString(fmt.Sprintf("rows:%v ", c))
		buf.WriteString(Reset)
	}

	colorSql := Magenta
	if level == slog.LevelError {
		colorSql = Red
	}

	buf.WriteString(colorSql)
	buf.WriteString(fmt.Sprintf("%v ", sql))
	buf.WriteString(Reset)

	buf.WriteString("\n")
}

func (h *handlerTextColor) appendCtxValue(buf *buffer, key, value string) {
	buf.WriteString(Faint)
	buf.WriteString(key + "=")
	buf.WriteString(Reset)
	buf.WriteString(value)
}

func (h *handlerTextColor) appendAttr(buf *buffer, attr slog.Attr, groupsPrefix string, groups []string) {
	attr.Value = attr.Value.Resolve()

	if attr.Equal(slog.Attr{}) {
		return
	}

	switch attr.Value.Kind() {
	case slog.KindAny:
		if err, ok := attr.Value.Any().(logError); ok {
			h.appendTintError(buf, err, attr.Key, groupsPrefix)
			buf.WriteByte(' ')
			return
		}
	case slog.KindGroup:
		if attr.Key != "" {
			groupsPrefix += attr.Key + "."
			groups = append(groups, attr.Key)
		}
		for _, groupAttr := range attr.Value.Group() {
			h.appendAttr(buf, groupAttr, groupsPrefix, groups)
		}
		return
	}

	h.appendKey(buf, attr.Key, groupsPrefix)
	h.appendValue(buf, attr.Value, true)
	buf.WriteByte(' ')
}

func (h *handlerTextColor) appendKey(buf *buffer, key, groups string) {
	buf.WriteString(Faint)
	appendString(buf, groups+key, false, true)
	buf.WriteByte('=')
	buf.WriteString(Reset)
}

func (h *handlerTextColor) appendValue(buf *buffer, v slog.Value, quote bool) {
	switch v.Kind() {
	case slog.KindString:
		appendString(buf, v.String(), quote, true)
	case slog.KindInt64:
		*buf = strconv.AppendInt(*buf, v.Int64(), 10)
	case slog.KindUint64:
		*buf = strconv.AppendUint(*buf, v.Uint64(), 10)
	case slog.KindFloat64:
		*buf = strconv.AppendFloat(*buf, v.Float64(), 'g', -1, 64)
	case slog.KindBool:
		*buf = strconv.AppendBool(*buf, v.Bool())
	case slog.KindDuration:
		appendString(buf, v.Duration().String(), quote, true)
	case slog.KindTime:
		appendString(buf, v.Time().String(), quote, true)
	case slog.KindAny:
		defer func() {
			// Copied from log/slog/handlerTextColor.go.
			if r := recover(); r != nil {
				// If it panics with a nil pointer, the most likely cases are
				// an encoding.TextMarshaler or error fails to guard against nil,
				// in which case "<nil>" seems to be the feasible choice.
				//
				// Adapted from the code in fmt/print.go.
				if v := reflect.ValueOf(v.Any()); v.Kind() == reflect.Pointer && v.IsNil() {
					appendString(buf, "<nil>", false, false)
					return
				}

				// Otherwise just print the original panic message.
				appendString(buf, fmt.Sprintf("!PANIC: %v", r), true, true)
			}
		}()

		switch cv := v.Any().(type) {
		case slog.Level:
			h.appendLevel(buf, cv)
		case encoding.TextMarshaler:
			data, err := cv.MarshalText()
			if err != nil {
				break
			}
			appendString(buf, string(data), quote, true)
		case *slog.Source:
			h.appendSource(buf, cv)
		default:
			appendString(buf, fmt.Sprintf("%+v", cv), quote, true)
		}
	}
}

func (h *handlerTextColor) appendTintError(buf *buffer, err logError, attrKey, groupsPrefix string) {
	buf.WriteString(Blue)
	appendString(buf, groupsPrefix+attrKey, true, true)
	buf.WriteByte('=')
	buf.WriteString(Faint)
	appendString(buf, err.Error(), true, true)
	buf.WriteString(Reset)
}

func appendString(buf *buffer, s string, quote, color bool) {
	if quote && !color {
		// trim ANSI escape sequences
		var inEscape bool
		s = cut(s, func(r rune) bool {
			if r == ansiEsc {
				inEscape = true
			} else if inEscape && unicode.IsLetter(r) {
				inEscape = false
				return true
			}

			return inEscape
		})
	}

	quote = quote && needsQuoting(s)
	switch {
	case color && quote:
		s = strconv.Quote(s)
		s = strings.ReplaceAll(s, `\x1b`, string(ansiEsc))
		buf.WriteString(s)
	case !color && quote:
		*buf = strconv.AppendQuote(*buf, s)
	default:
		buf.WriteString(s)
	}
}

func cut(s string, f func(r rune) bool) string {
	var res []rune
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError {
			break
		}
		if !f(r) {
			res = append(res, r)
		}
		i += size
	}
	return string(res)
}

// Copied from log/slog/text_handler.go.
func needsQuoting(s string) bool {
	if len(s) == 0 {
		return true
	}
	for i := 0; i < len(s); {
		b := s[i]
		if b < utf8.RuneSelf {
			// Quote anything except a backslash that would need quoting in a
			// JSON string, as well as space and '='
			if b != '\\' && (b == ' ' || b == '=' || !safeSet[b]) {
				return true
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError || unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return true
		}
		i += size
	}
	return false
}

type logError struct{ error }

var safeSet = [utf8.RuneSelf]bool{
	' ':      true,
	'!':      true,
	'"':      false,
	'#':      true,
	'$':      true,
	'%':      true,
	'&':      true,
	'\'':     true,
	'(':      true,
	')':      true,
	'*':      true,
	'+':      true,
	',':      true,
	'-':      true,
	'.':      true,
	'/':      true,
	'0':      true,
	'1':      true,
	'2':      true,
	'3':      true,
	'4':      true,
	'5':      true,
	'6':      true,
	'7':      true,
	'8':      true,
	'9':      true,
	':':      true,
	';':      true,
	'<':      true,
	'=':      true,
	'>':      true,
	'?':      true,
	'@':      true,
	'A':      true,
	'B':      true,
	'C':      true,
	'D':      true,
	'E':      true,
	'F':      true,
	'G':      true,
	'H':      true,
	'I':      true,
	'J':      true,
	'K':      true,
	'L':      true,
	'M':      true,
	'N':      true,
	'O':      true,
	'P':      true,
	'Q':      true,
	'R':      true,
	'S':      true,
	'T':      true,
	'U':      true,
	'V':      true,
	'W':      true,
	'X':      true,
	'Y':      true,
	'Z':      true,
	'[':      true,
	'\\':     false,
	']':      true,
	'^':      true,
	'_':      true,
	'`':      true,
	'a':      true,
	'b':      true,
	'c':      true,
	'd':      true,
	'e':      true,
	'f':      true,
	'g':      true,
	'h':      true,
	'i':      true,
	'j':      true,
	'k':      true,
	'l':      true,
	'm':      true,
	'n':      true,
	'o':      true,
	'p':      true,
	'q':      true,
	'r':      true,
	's':      true,
	't':      true,
	'u':      true,
	'v':      true,
	'w':      true,
	'x':      true,
	'y':      true,
	'z':      true,
	'{':      true,
	'|':      true,
	'}':      true,
	'~':      true,
	'\u007f': true,
	'\u001b': true,
}
