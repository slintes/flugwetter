package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

// The weather payload is ~130KB of highly repetitive JSON -- 168 hours by up to 19 cloud
// levels and 6 wind layers -- refetched every 15 minutes and on every airport switch,
// usually over mobile. The JS modules and the vendored libraries add another ~450KB on a
// cold load. All of it compresses by roughly 80-90%.

// gzipMinSize is the smallest response worth compressing. Below about a packet's worth the
// gzip header and the CPU cost buy nothing, and very small responses can even grow.
const gzipMinSize = 1024

// gzipWriterPool avoids allocating a compressor per request.
var gzipWriterPool = sync.Pool{
	New: func() any {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
		return w
	},
}

// gzipResponseWriter buffers until it knows whether the response is worth compressing.
//
// The decision needs the Content-Type and the size, and neither is known when the
// middleware runs -- only once the handler writes. So the first Write decides, and
// everything before that is held back.
type gzipResponseWriter struct {
	http.ResponseWriter

	statusCode  int
	wroteHeader bool

	// gz is non-nil once compression has started.
	gz *gzip.Writer
	// buf holds the first bytes while the decision is pending.
	buf []byte
	// decided is set once the compress-or-not choice has been made.
	decided bool
}

// WriteHeader records the status without forwarding it.
//
// Forwarding here would commit the headers before the compress-or-not decision has been
// made, and Content-Encoding is set as part of that decision. http.ServeContent -- which
// serves every embedded asset -- calls WriteHeader before its first Write, so an
// immediate pass-through sent a gzipped body with no Content-Encoding header at all.
func (w *gzipResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.statusCode = code
	w.wroteHeader = true
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.decided {
		w.buf = append(w.buf, b...)
		// Wait for enough bytes to judge the size, unless the handler has already declared
		// a length.
		if len(w.buf) < gzipMinSize && w.Header().Get("Content-Length") == "" {
			return len(b), nil
		}
		// decide() flushes the buffer, and the buffer already contains b -- writing b again
		// here would duplicate this chunk in the response.
		w.decide()
		return len(b), nil
	}

	if w.gz != nil {
		return w.gz.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

// decide chooses compression based on what the handler set, then flushes whatever was
// buffered.
func (w *gzipResponseWriter) decide() {
	w.decided = true

	if compressibleType(w.Header().Get("Content-Type")) && len(w.buf) >= gzipMinSize {
		w.Header().Set("Content-Encoding", "gzip")
		// Caches must not hand a gzipped body to a client that did not ask for one.
		w.Header().Add("Vary", "Accept-Encoding")
		// Content-Length describes the uncompressed body and is now wrong. Left in place
		// when not compressing, where it is still correct.
		w.Header().Del("Content-Length")

		gz := gzipWriterPool.Get().(*gzip.Writer)
		gz.Reset(w.ResponseWriter)
		w.gz = gz
	}

	// Now, and only now, are the headers final.
	w.ResponseWriter.WriteHeader(w.statusOrOK())

	if len(w.buf) > 0 {
		if w.gz != nil {
			_, _ = w.gz.Write(w.buf)
		} else {
			_, _ = w.ResponseWriter.Write(w.buf)
		}
		w.buf = nil
	}
}

func (w *gzipResponseWriter) statusOrOK() int {
	if w.statusCode == 0 {
		return http.StatusOK
	}
	return w.statusCode
}

// close flushes a short response that never reached the decision point, and returns the
// compressor to the pool.
func (w *gzipResponseWriter) close() {
	if !w.decided {
		w.decide()
	}
	if w.gz != nil {
		_ = w.gz.Close()
		gzipWriterPool.Put(w.gz)
		w.gz = nil
	}
}

// compressibleType reports whether a Content-Type is worth compressing. PNG tiles and the
// icons are already compressed; running them through gzip costs CPU and saves nothing.
func compressibleType(contentType string) bool {
	mediaType, _, _ := strings.Cut(contentType, ";")
	mediaType = strings.TrimSpace(strings.ToLower(mediaType))

	switch {
	case mediaType == "":
		return false
	case strings.HasPrefix(mediaType, "text/"):
		return true
	case mediaType == "application/json",
		mediaType == "application/javascript",
		mediaType == "text/javascript",
		mediaType == "image/svg+xml":
		return true
	default:
		return false
	}
}

// gzipMiddleware compresses responses for clients that accept it.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptsGzip(r) {
			next.ServeHTTP(w, r)
			return
		}

		gw := &gzipResponseWriter{ResponseWriter: w}
		defer gw.close()

		next.ServeHTTP(gw, r)
	})
}

// acceptsGzip reports whether the client offered gzip. "identity;q=0" style negotiation is
// not handled -- no browser sends it, and guessing wrong would break the response.
func acceptsGzip(r *http.Request) bool {
	for _, encoding := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, _, _ := strings.Cut(strings.TrimSpace(encoding), ";")
		if strings.EqualFold(name, "gzip") {
			return true
		}
	}
	return false
}
