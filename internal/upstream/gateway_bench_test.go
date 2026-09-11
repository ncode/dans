package upstream

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ncode/dans/api"
	"github.com/ncode/dans/internal/httpapi"
)

// Deliberately exposes only Read: a bytes.Reader WriteTo shortcut is not a
// representative source for the network relay.
type benchmarkBody struct {
	reader *bytes.Reader
	closed bool
}

func (body *benchmarkBody) Read(p []byte) (int, error) { return body.reader.Read(p) }
func (body *benchmarkBody) Close() error               { body.closed = true; return nil }

type benchmarkWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *benchmarkWriter) Header() http.Header         { return w.header }
func (w *benchmarkWriter) WriteHeader(status int)      { w.status = status }
func (w *benchmarkWriter) Write(p []byte) (int, error) { return w.body.Write(p) }
func (w *benchmarkWriter) reset()                      { clear(w.header); w.status = 0; w.body.Reset() }

func benchmarkHeaders(filtered bool) http.Header {
	header := http.Header{"Content-Type": {"application/octet-stream"}}
	if filtered {
		header.Set("Connection", "X-Private")
		header.Set("X-Private", "remove")
		header.Set("X-Api-Key", "remove")
		header.Set("X-Request-Id", "replace")
		header.Set("Cache-Control", "replace")
	}
	return header
}

func checkBenchmarkRelay(b *testing.B, w *benchmarkWriter, payload []byte, status int) {
	b.Helper()
	if w.status != status || !bytes.Equal(w.body.Bytes(), payload) ||
		w.header.Get("Content-Type") != "application/octet-stream" ||
		w.header.Get("X-Request-Id") != "benchmark" || w.header.Get("Cache-Control") != "no-store" ||
		w.header.Get("Connection") != "" || w.header.Get("X-Private") != "" || w.header.Get("X-Api-Key") != "" {
		b.Fatal("relay changed response bytes, status, or header policy")
	}
}

func BenchmarkRelay(b *testing.B) {
	for _, size := range []int{1 << 10, 64 << 10, 1 << 20} {
		for _, filtered := range []bool{false, true} {
			b.Run(strconv.Itoa(size)+"/Filtered="+strconv.FormatBool(filtered), func(b *testing.B) {
				payload := bytes.Repeat([]byte("x"), size)
				body := benchmarkBody{reader: bytes.NewReader(payload)}
				status := http.StatusOK
				if filtered {
					status = http.StatusBadGateway
				}
				response := http.Response{StatusCode: status, Header: benchmarkHeaders(filtered), Body: &body}
				writer := benchmarkWriter{header: make(http.Header)}
				writer.body.Grow(size)
				b.ReportAllocs()
				b.SetBytes(int64(size))
				for b.Loop() {
					writer.reset()
					body.reader.Reset(payload)
					body.closed = false
					if err := Relay(&writer, &response, "benchmark"); err != nil {
						b.Fatal(err)
					}
					if !body.closed {
						b.Fatal("relay did not close body")
					}
					checkBenchmarkRelay(b, &writer, payload, status)
				}
				checkBenchmarkRelay(b, &writer, payload, status)
			})
		}
	}
}

func BenchmarkForwardLoopback(b *testing.B) {
	for _, size := range []int{1 << 10, 64 << 10, 1 << 20} {
		for _, status := range []int{http.StatusOK, http.StatusBadGateway} {
			b.Run(strconv.Itoa(size)+"/Status="+strconv.Itoa(status), func(b *testing.B) {
				payload := bytes.Repeat([]byte("x"), size)
				var connections atomic.Int64
				var badRequest atomic.Bool
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet || r.URL.Path != "/api/v1/servers/localhost/zones/example.org." || r.Header.Get("X-Api-Key") != "benchmark-key" {
						badRequest.Store(true)
					}
					for key, values := range benchmarkHeaders(true) {
						w.Header()[key] = values
					}
					w.WriteHeader(status)
					if _, err := w.Write(payload); err != nil {
						badRequest.Store(true)
					}
				}))
				server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
					if state == http.StateNew {
						connections.Add(1)
					}
				}
				server.Start()
				b.Cleanup(server.Close)
				client, err := New(TransportConfig{URL: server.URL, Timeout: 5 * time.Second}, httpapi.NewSecret("benchmark-key"))
				if err != nil {
					b.Fatal(err)
				}
				httpClient := client.generated.Client.(*http.Client)
				transport := httpClient.Transport.(authenticatedTransport).next.(*http.Transport)
				b.Cleanup(transport.CloseIdleConnections)
				operation := func(client api.ClientInterface) (*http.Response, error) {
					return client.ListZone(b.Context(), "localhost", "example.org.", nil)
				}
				writer := benchmarkWriter{header: make(http.Header)}
				writer.body.Grow(size)
				invoke := func() {
					writer.reset()
					response, err := client.Forward(operation)
					if err != nil {
						b.Fatal(err)
					}
					if err := Relay(&writer, response, "benchmark"); err != nil {
						b.Fatal(err)
					}
					checkBenchmarkRelay(b, &writer, payload, status)
				}
				invoke()
				warm := connections.Load()
				b.ReportAllocs()
				b.SetBytes(int64(size))
				for b.Loop() {
					invoke()
				}
				checkBenchmarkRelay(b, &writer, payload, status)
				if badRequest.Load() {
					b.Fatal("upstream request or response failed")
				}
				if connections.Load() != warm {
					b.Fatal("warm connection was not reused")
				}
				b.ReportMetric(float64(connections.Load()-warm)/float64(b.N), "conns/op")
			})
		}
	}
}
