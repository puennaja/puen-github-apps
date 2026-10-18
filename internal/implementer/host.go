package implementer

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"
	"syscall"
)

type JSONLAudit struct {
	file *os.File
	mu   sync.Mutex
}

func OpenJSONLAudit(path string) (*JSONLAudit, error) {
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_APPEND|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, fail("audit_unavailable")
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fail("audit_unavailable")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || int(owner.Uid) != os.Geteuid() {
		file.Close()
		return nil, fail("audit_unavailable")
	}
	return &JSONLAudit{file: file}, nil
}
func (a *JSONLAudit) Append(event AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	encoded, err := json.Marshal(event)
	if err != nil {
		return fail("audit_unavailable")
	}
	encoded = append(encoded, '\n')
	n, err := a.file.Write(encoded)
	if err != nil || n != len(encoded) {
		return fail("audit_unavailable")
	}
	if err := a.file.Sync(); err != nil {
		return fail("audit_unavailable")
	}
	return nil
}
func (a *JSONLAudit) Close() error { return a.file.Close() }

type wireRequest struct {
	Operation string          `json:"operation"`
	Arguments json.RawMessage `json:"arguments"`
}

func Serve(ctx context.Context, g *Gateway, in io.Reader, out io.Writer) error {
	r := bufio.NewReaderSize(in, 1_048_577)
	w := bufio.NewWriter(out)
	defer w.Flush()
	for {
		line, err := r.ReadSlice('\n')
		if err == bufio.ErrBufferFull || len(line) > 1_048_576 {
			writeReply(w, map[string]any{"error": "request_invalid"})
			return nil
		}
		if err == io.EOF && len(line) == 0 {
			return nil
		}
		if err != nil && err != io.EOF {
			return fail("request_invalid")
		}
		var req wireRequest
		if decodeArguments(line, &req) != nil || req.Operation == "" || len(req.Arguments) == 0 {
			writeReply(w, map[string]any{"error": "request_invalid"})
			if err == io.EOF {
				return nil
			}
			continue
		}
		result, executeErr := g.Execute(ctx, req.Operation, req.Arguments)
		if executeErr != nil {
			writeReply(w, map[string]any{"error": safeCode(executeErr)})
		} else {
			writeReply(w, map[string]any{"result": result})
		}
		if err == io.EOF {
			return nil
		}
	}
}
func writeReply(w *bufio.Writer, value any) {
	encoded, _ := json.Marshal(value)
	w.Write(encoded)
	w.WriteByte('\n')
	w.Flush()
}
