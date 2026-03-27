package http

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

const (
	structuredKeyvalCapacity = 16
)

var errNotHijacker = errors.New("http: response writer does not implement http.Hijacker")

// LogFormat controls the output format of the logging middleware.
type LogFormat int

const (
	// FormatStructured emits structured key-value fields via logger.Logger.
	FormatStructured LogFormat = iota

	// FormatCommon emits NCSA Common Log Format (CLF).
	FormatCommon

	// FormatCombined emits NCSA Combined Log Format (CLF + Referer + User-Agent).
	FormatCombined

	// FormatJSON emits a single JSON object per request.
	FormatJSON
)

// LoggingOption configures transport logging behaviour.
type LoggingOption func(*loggingConfig)

type loggingConfig struct {
	format       LogFormat
	level        logger.Level
	logLatency   bool
	logUserAgent bool
	pathFilter   map[string]struct{}
	headerFields []string
}

func defaultLoggingConfig() loggingConfig {
	return loggingConfig{
		format:       FormatStructured,
		level:        logger.InfoLevel,
		logLatency:   true,
		logUserAgent: true,
	}
}

// WithFormat sets the log output format. Defaults to FormatStructured.
func WithFormat(format LogFormat) LoggingOption {
	return func(c *loggingConfig) {
		c.format = format
	}
}

// WithLogLevel sets the log level for successful requests.
// Defaults to logger.InfoLevel. Errors always log at logger.ErrorLevel.
func WithLogLevel(level logger.Level) LoggingOption {
	return func(c *loggingConfig) {
		c.level = level
	}
}

// WithoutLatency disables the "latency" field.
func WithoutLatency() LoggingOption {
	return func(c *loggingConfig) {
		c.logLatency = false
	}
}

// WithoutUserAgent disables the "user_agent" field.
func WithoutUserAgent() LoggingOption {
	return func(c *loggingConfig) {
		c.logUserAgent = false
	}
}

// WithPathFilter excludes requests matching the given paths from logging.
func WithPathFilter(paths ...string) LoggingOption {
	return func(c *loggingConfig) {
		if c.pathFilter == nil {
			c.pathFilter = make(map[string]struct{}, len(paths))
		}

		for _, p := range paths {
			c.pathFilter[p] = struct{}{}
		}
	}
}

// WithHeaderFields logs the specified request header values as fields.
// Header names are normalised to lowercase. Values are truncated to 256 bytes.
func WithHeaderFields(headers ...string) LoggingOption {
	return func(c *loggingConfig) {
		c.headerFields = append(c.headerFields, headers...)
	}
}

// LoggingMiddleware returns an HTTP Middleware that logs each completed request.
func LoggingMiddleware(l logger.Logger, opts ...LoggingOption) Middleware {
	cfg := defaultLoggingConfig()
	for _, o := range opts {
		o(&cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, filtered := cfg.pathFilter[r.URL.Path]; filtered {
				next.ServeHTTP(w, r)

				return
			}

			start := time.Now()
			rl := &responseLogger{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(rl, r)

			data := requestData{
				method:    r.Method,
				path:      r.URL.Path,
				proto:     r.Proto,
				status:    rl.statusCode,
				bytes:     rl.bytesWritten,
				latency:   time.Since(start),
				clientIP:  clientIP(r),
				userAgent: r.UserAgent(),
				referer:   r.Referer(),
				timestamp: start,
				headers:   extractHeaders(r, cfg.headerFields),
			}

			level := cfg.level
			if data.status >= http.StatusInternalServerError {
				level = logger.ErrorLevel
			}

			switch cfg.format {
			case FormatCommon:
				emitCommon(l, level, data)
			case FormatCombined:
				emitCombined(l, level, cfg, data)
			case FormatJSON:
				emitJSON(l, level, cfg, data)
			case FormatStructured:
				emitStructured(l, level, cfg, data)
			}
		})
	}
}

type requestData struct {
	method    string
	path      string
	proto     string
	status    int
	bytes     int
	latency   time.Duration
	clientIP  string
	userAgent string
	referer   string
	timestamp time.Time
	headers   map[string]string
}

func emitStructured(l logger.Logger, level logger.Level, cfg loggingConfig, d requestData) {
	keyvals := make([]any, 0, structuredKeyvalCapacity)
	keyvals = append(keyvals, "method", d.method, "path", d.path, "status", d.status, "bytes", d.bytes)

	if cfg.logLatency {
		keyvals = append(keyvals, "latency", d.latency.String())
	}

	keyvals = append(keyvals, "client_ip", d.clientIP)

	if cfg.logUserAgent {
		keyvals = append(keyvals, "user_agent", d.userAgent)
	}

	for k, v := range d.headers {
		keyvals = append(keyvals, k, v)
	}

	logAtLevel(l.With(keyvals...), level, "request completed")
}

const (
	headerMaxLen = 256
	clfTimeFmt   = "02/Jan/2006:15:04:05 -0700"
)

func emitCommon(l logger.Logger, level logger.Level, d requestData) {
	line := fmt.Sprintf(`%s - - [%s] "%s %s %s" %d %d`,
		d.clientIP,
		d.timestamp.Format(clfTimeFmt),
		d.method, d.path, d.proto,
		d.status, d.bytes,
	)

	logAtLevel(l, level, line)
}

func emitCombined(l logger.Logger, level logger.Level, cfg loggingConfig, d requestData) {
	ua := d.userAgent
	if !cfg.logUserAgent {
		ua = "-"
	}

	referer := d.referer
	if referer == "" {
		referer = "-"
	}

	line := fmt.Sprintf(`%s - - [%s] "%s %s %s" %d %d "%s" "%s"`,
		d.clientIP,
		d.timestamp.Format(clfTimeFmt),
		d.method, d.path, d.proto,
		d.status, d.bytes,
		referer, ua,
	)

	logAtLevel(l, level, line)
}

func emitJSON(l logger.Logger, level logger.Level, cfg loggingConfig, d requestData) {
	m := map[string]any{
		"timestamp": d.timestamp.UTC().Format(time.RFC3339Nano),
		"method":    d.method,
		"path":      d.path,
		"status":    d.status,
		"bytes":     d.bytes,
		"client_ip": d.clientIP,
	}

	if cfg.logLatency {
		m["latency"] = d.latency.String()
	}

	if cfg.logUserAgent {
		m["user_agent"] = d.userAgent
	}

	for k, v := range d.headers {
		m[k] = v
	}

	b, err := json.Marshal(m)
	if err != nil {
		l.Error("failed to marshal request log", "error", err)

		return
	}

	logAtLevel(l, level, string(b))
}

func logAtLevel(l logger.Logger, level logger.Level, msg string) {
	switch level {
	case logger.DebugLevel:
		l.Debug(msg)
	case logger.InfoLevel:
		l.Info(msg)
	case logger.WarnLevel:
		l.Warn(msg)
	case logger.ErrorLevel:
		l.Error(msg)
	case logger.FatalLevel:
		l.Fatal(msg)
	}
}

func extractHeaders(r *http.Request, fields []string) map[string]string {
	if len(fields) == 0 {
		return nil
	}

	m := make(map[string]string, len(fields))

	for _, h := range fields {
		v := r.Header.Get(h)
		if v == "" {
			continue
		}

		if len(v) > headerMaxLen {
			v = v[:headerMaxLen]
		}

		m[strings.ToLower(h)] = v
	}

	return m
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(ip)
		}

		return strings.TrimSpace(xff)
	}

	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}

// responseLogger wraps http.ResponseWriter to capture status code and bytes written.
type responseLogger struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
	wroteHeader  bool
}

func (rl *responseLogger) WriteHeader(code int) {
	if !rl.wroteHeader {
		rl.statusCode = code
		rl.wroteHeader = true
	}

	rl.ResponseWriter.WriteHeader(code)
}

func (rl *responseLogger) Write(b []byte) (int, error) {
	if !rl.wroteHeader {
		rl.WriteHeader(http.StatusOK)
	}

	n, err := rl.ResponseWriter.Write(b)
	rl.bytesWritten += n

	return n, err
}

// Flush implements http.Flusher if the underlying writer supports it.
func (rl *responseLogger) Flush() {
	if f, ok := rl.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker if the underlying writer supports it.
func (rl *responseLogger) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := rl.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}

	return nil, nil, errNotHijacker
}
